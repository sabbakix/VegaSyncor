// Package s3test is a small S3 server for the tests: objects in memory, path-style
// addressing, AWS Signature V4 checked for real (so wrong credentials are refused)
// and Content-MD5 verified. It lets a test break an upload on purpose and count
// the requests, which a real server cannot do. It is not a complete S3: the
// behaviour it copies is the part rclone uses, and it is no substitute for a run
// against a real provider (see VEGASYNCOR_TEST_S3_* in internal/cloud).
package s3test

import (
	"bytes"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Object is a stored object.
type Object struct {
	Data     []byte
	Meta     map[string]string // x-amz-meta-*, lower case, without the prefix
	Modified time.Time
	ETag     string
}

type multipart struct {
	bucket, key string
	meta        map[string]string
	parts       map[int][]byte
}

// Server is the fake S3 endpoint.
type Server struct {
	*httptest.Server
	AccessKey, Secret string

	mu       sync.Mutex
	buckets  map[string]map[string]*Object
	uploads  map[string]*multipart
	nextID   int
	requests []string // "METHOD bucket/key?query"
	// FailPut, if set, makes the upload of the keys it accepts fail with a 500.
	FailPut func(key string) bool
	// FailList, if set, makes every listing fail with a 500.
	FailList bool
	// Hang, if set, is closed to release the requests kept waiting: no answer until then.
	Hang chan struct{}
}

// New starts a server with the given credentials and empty buckets.
func New(accessKey, secret string, buckets ...string) *Server {
	s := &Server{AccessKey: accessKey, Secret: secret,
		buckets: map[string]map[string]*Object{}, uploads: map[string]*multipart{}}
	for _, b := range buckets {
		s.buckets[b] = map[string]*Object{}
	}
	s.Server = httptest.NewServer(http.HandlerFunc(s.handle))
	return s
}

// Keys returns the sorted keys in a bucket.
func (s *Server) Keys(bucket string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := []string{}
	for k := range s.buckets[bucket] {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Get returns an object (nil if missing).
func (s *Server) Get(bucket, key string) *Object {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buckets[bucket][key]
}

// Put stores an object directly, as a tool other than rclone would.
func (s *Server) Put(bucket, key string, data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sum := md5.Sum(data)
	s.buckets[bucket][key] = &Object{Data: data, Meta: map[string]string{}, Modified: time.Now(), ETag: hex.EncodeToString(sum[:])}
}

// HasBucket reports whether a bucket exists.
func (s *Server) HasBucket(b string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buckets[b] != nil
}

// Requests returns the requests received so far.
func (s *Server) Requests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.requests...)
}

// PendingUploads is the number of multipart uploads not completed or aborted.
func (s *Server) PendingUploads() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.uploads)
}

func xmlError(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?><Error><Code>%s</Code><Message>%s</Message></Error>`, code, msg)
}

func xmlReply(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/xml")
	io.WriteString(w, xml.Header)
	xml.NewEncoder(w).Encode(v)
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	if code := s.checkSignature(r); code != "" {
		xmlError(w, 403, code, "the request signature is not valid")
		return
	}
	if h := s.hang(); h != nil && r.Method == http.MethodPut {
		<-h
		xmlError(w, 500, "InternalError", "released by the test")
		return
	}
	// the path is decoded by net/http: bucket and key are what the client meant
	bucket, key, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")

	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = append(s.requests, r.Method+" "+bucket+"/"+key+"?"+r.URL.RawQuery)
	q := r.URL.Query()

	if bucket == "" {
		s.listBuckets(w)
		return
	}
	if key == "" && r.Method == http.MethodPut {
		if s.buckets[bucket] == nil {
			s.buckets[bucket] = map[string]*Object{}
		}
		return
	}
	objects := s.buckets[bucket]
	if objects == nil {
		xmlError(w, 404, "NoSuchBucket", "The specified bucket does not exist")
		return
	}
	switch {
	case key == "" && r.Method == http.MethodHead:
	case key == "" && r.Method == http.MethodGet && q.Has("location"):
		xmlReply(w, struct {
			XMLName xml.Name `xml:"LocationConstraint"`
		}{})
	case key == "" && r.Method == http.MethodGet:
		s.list(w, q, objects)
	case key == "" && r.Method == http.MethodPost && q.Has("delete"):
		s.deleteMany(w, r, objects)
	case r.Method == http.MethodPost && q.Has("uploads"):
		s.nextID++
		id := "up" + strconv.Itoa(s.nextID)
		s.uploads[id] = &multipart{bucket: bucket, key: key, meta: metaOf(r), parts: map[int][]byte{}}
		xmlReply(w, struct {
			XMLName  xml.Name `xml:"InitiateMultipartUploadResult"`
			Bucket   string
			Key      string
			UploadId string
		}{Bucket: bucket, Key: key, UploadId: id})
	case r.Method == http.MethodPut && q.Get("uploadId") != "":
		s.putPart(w, r, q, key)
	case r.Method == http.MethodPost && q.Get("uploadId") != "":
		s.completeUpload(w, r, q, objects)
	case r.Method == http.MethodDelete && q.Get("uploadId") != "":
		delete(s.uploads, q.Get("uploadId"))
		w.WriteHeader(204)
	case r.Method == http.MethodPut && r.Header.Get("X-Amz-Copy-Source") != "":
		s.copyObject(w, r, objects, key)
	case r.Method == http.MethodPut:
		s.putObject(w, r, objects, key)
	case r.Method == http.MethodGet || r.Method == http.MethodHead:
		s.getObject(w, r, objects, key)
	case r.Method == http.MethodDelete:
		delete(objects, key)
		w.WriteHeader(204)
	default:
		xmlError(w, 501, "NotImplemented", r.Method)
	}
}

func (s *Server) hang() chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Hang
}

func metaOf(r *http.Request) map[string]string {
	m := map[string]string{}
	for k, v := range r.Header {
		if lk := strings.ToLower(k); strings.HasPrefix(lk, "x-amz-meta-") && len(v) > 0 {
			m[strings.TrimPrefix(lk, "x-amz-meta-")] = v[0]
		}
	}
	return m
}

func (s *Server) failPut(w http.ResponseWriter, key string) bool {
	if s.FailPut != nil && s.FailPut(key) {
		xmlError(w, 500, "InternalError", "upload broken by the test")
		return true
	}
	return false
}

// readBody reads the body and checks it against the checksums the client sent.
func readBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	data, err := io.ReadAll(r.Body)
	if err != nil {
		xmlError(w, 400, "IncompleteBody", err.Error())
		return nil, false
	}
	if want := r.Header.Get("Content-MD5"); want != "" {
		sum := md5.Sum(data)
		if base64.StdEncoding.EncodeToString(sum[:]) != want {
			xmlError(w, 400, "BadDigest", "Content-MD5 mismatch")
			return nil, false
		}
	}
	if want := r.Header.Get("X-Amz-Content-Sha256"); want != "" && want != "UNSIGNED-PAYLOAD" {
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != want {
			xmlError(w, 400, "XAmzContentSHA256Mismatch", "payload hash mismatch")
			return nil, false
		}
	}
	return data, true
}

func (s *Server) putObject(w http.ResponseWriter, r *http.Request, objects map[string]*Object, key string) {
	data, ok := readBody(w, r)
	if !ok || s.failPut(w, key) {
		return
	}
	sum := md5.Sum(data)
	o := &Object{Data: data, Meta: metaOf(r), Modified: time.Now().UTC(), ETag: hex.EncodeToString(sum[:])}
	objects[key] = o
	w.Header().Set("ETag", `"`+o.ETag+`"`)
}

func (s *Server) putPart(w http.ResponseWriter, r *http.Request, q url.Values, key string) {
	up := s.uploads[q.Get("uploadId")]
	if up == nil {
		xmlError(w, 404, "NoSuchUpload", "no such upload")
		return
	}
	data, ok := readBody(w, r)
	if !ok || s.failPut(w, key) {
		return
	}
	n, _ := strconv.Atoi(q.Get("partNumber"))
	up.parts[n] = data
	sum := md5.Sum(data)
	w.Header().Set("ETag", `"`+hex.EncodeToString(sum[:])+`"`)
}

func (s *Server) completeUpload(w http.ResponseWriter, r *http.Request, q url.Values, objects map[string]*Object) {
	id := q.Get("uploadId")
	up := s.uploads[id]
	if up == nil {
		xmlError(w, 404, "NoSuchUpload", "no such upload")
		return
	}
	var body struct {
		Part []struct{ PartNumber int }
	}
	if err := xml.NewDecoder(r.Body).Decode(&body); err != nil {
		xmlError(w, 400, "MalformedXML", err.Error())
		return
	}
	var data []byte
	for _, p := range body.Part {
		part, ok := up.parts[p.PartNumber]
		if !ok {
			xmlError(w, 400, "InvalidPart", "missing part")
			return
		}
		data = append(data, part...)
	}
	sum := md5.Sum(data)
	// like S3: the ETag of a multipart object is not the MD5 of its content
	o := &Object{Data: data, Meta: up.meta, Modified: time.Now().UTC(),
		ETag: fmt.Sprintf("%s-%d", hex.EncodeToString(sum[:]), len(body.Part))}
	objects[up.key] = o
	delete(s.uploads, id)
	xmlReply(w, struct {
		XMLName xml.Name `xml:"CompleteMultipartUploadResult"`
		Key     string
		ETag    string
	}{Key: up.key, ETag: `"` + o.ETag + `"`})
}

func (s *Server) copyObject(w http.ResponseWriter, r *http.Request, objects map[string]*Object, key string) {
	src, err := url.PathUnescape(strings.TrimPrefix(r.Header.Get("X-Amz-Copy-Source"), "/"))
	if err != nil {
		xmlError(w, 400, "InvalidArgument", err.Error())
		return
	}
	srcBucket, srcKey, _ := strings.Cut(src, "/")
	o := s.buckets[srcBucket][srcKey]
	if o == nil {
		xmlError(w, 404, "NoSuchKey", "copy source not found")
		return
	}
	meta := o.Meta
	if strings.EqualFold(r.Header.Get("X-Amz-Metadata-Directive"), "REPLACE") {
		meta = metaOf(r)
	}
	n := &Object{Data: o.Data, Meta: meta, Modified: time.Now().UTC(), ETag: o.ETag}
	objects[key] = n
	xmlReply(w, struct {
		XMLName      xml.Name `xml:"CopyObjectResult"`
		ETag         string
		LastModified string
	}{ETag: `"` + n.ETag + `"`, LastModified: n.Modified.Format(time.RFC3339)})
}

func (s *Server) getObject(w http.ResponseWriter, r *http.Request, objects map[string]*Object, key string) {
	o := objects[key]
	if o == nil {
		if r.Method == http.MethodHead {
			w.WriteHeader(404)
			return
		}
		xmlError(w, 404, "NoSuchKey", "The specified key does not exist")
		return
	}
	for k, v := range o.Meta {
		w.Header()["X-Amz-Meta-"+strings.ToUpper(k[:1])+k[1:]] = []string{v}
	}
	w.Header().Set("ETag", `"`+o.ETag+`"`)
	http.ServeContent(w, r, "", o.Modified, bytes.NewReader(o.Data))
}

func (s *Server) listBuckets(w http.ResponseWriter) {
	type bucket struct{ Name, CreationDate string }
	var out struct {
		XMLName xml.Name `xml:"ListAllMyBucketsResult"`
		Buckets struct {
			Bucket []bucket
		}
	}
	for b := range s.buckets {
		out.Buckets.Bucket = append(out.Buckets.Bucket, bucket{b, time.Now().UTC().Format(time.RFC3339)})
	}
	xmlReply(w, out)
}

func (s *Server) deleteMany(w http.ResponseWriter, r *http.Request, objects map[string]*Object) {
	var body struct {
		Object []struct{ Key string }
	}
	if err := xml.NewDecoder(r.Body).Decode(&body); err != nil {
		xmlError(w, 400, "MalformedXML", err.Error())
		return
	}
	type deleted struct{ Key string }
	var out struct {
		XMLName xml.Name `xml:"DeleteResult"`
		Deleted []deleted
	}
	for _, o := range body.Object {
		delete(objects, o.Key)
		out.Deleted = append(out.Deleted, deleted{o.Key})
	}
	xmlReply(w, out)
}

// list answers ListObjects (v1 and v2), with prefix, delimiter and paging.
func (s *Server) list(w http.ResponseWriter, q url.Values, objects map[string]*Object) {
	if s.FailList {
		xmlError(w, 500, "InternalError", "listing broken by the test")
		return
	}
	prefix, delim := q.Get("prefix"), q.Get("delimiter")
	maxKeys := 1000
	if n, err := strconv.Atoi(q.Get("max-keys")); err == nil && n > 0 && n < maxKeys {
		maxKeys = n
	}
	after := q.Get("marker")
	if v := q.Get("start-after"); v != "" {
		after = v
	}
	if v := q.Get("continuation-token"); v != "" {
		after = v
	}
	keys := make([]string, 0, len(objects))
	for k := range objects {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)

	enc := func(k string) string {
		if q.Get("encoding-type") == "url" {
			return strings.ReplaceAll(url.QueryEscape(k), "+", "%20")
		}
		return k
	}
	type content struct {
		Key          string
		LastModified string
		ETag         string
		Size         int
		StorageClass string
	}
	type common struct{ Prefix string }
	var contents []content
	var prefixes []common
	seen := map[string]bool{}
	truncated, last := false, ""
	for _, k := range keys {
		name := k
		isPrefix := false
		if delim != "" {
			if i := strings.Index(k[len(prefix):], delim); i >= 0 {
				name, isPrefix = k[:len(prefix)+i+len(delim)], true
			}
		}
		if name <= after || seen[name] {
			continue
		}
		if len(contents)+len(prefixes) >= maxKeys {
			truncated = true
			break
		}
		last = name
		if isPrefix {
			seen[name] = true
			prefixes = append(prefixes, common{enc(name)})
			continue
		}
		o := objects[k]
		contents = append(contents, content{enc(k), o.Modified.UTC().Format("2006-01-02T15:04:05.000Z"), `"` + o.ETag + `"`, len(o.Data), "STANDARD"})
	}
	out := struct {
		XMLName               xml.Name `xml:"ListBucketResult"`
		Name                  string
		Prefix                string
		IsTruncated           bool
		NextMarker            string `xml:",omitempty"`
		NextContinuationToken string `xml:",omitempty"`
		Contents              []content
		CommonPrefixes        []common
	}{Name: "bucket", Prefix: enc(prefix), IsTruncated: truncated, Contents: contents, CommonPrefixes: prefixes}
	if truncated {
		if q.Get("list-type") == "2" {
			out.NextContinuationToken = last
		} else {
			out.NextMarker = enc(last)
		}
	}
	xmlReply(w, out)
}

// --- Signature V4 ---

var credentialRe = regexp.MustCompile(`Credential=([^/]+)/(\d{8})/([^/]+)/s3/aws4_request,\s*SignedHeaders=([^,]+),\s*Signature=([0-9a-f]+)`)

// checkSignature returns the S3 error code of a request with a wrong signature.
func (s *Server) checkSignature(r *http.Request) string {
	m := credentialRe.FindStringSubmatch(r.Header.Get("Authorization"))
	if m == nil {
		return "AccessDenied"
	}
	key, date, region, signed, got := m[1], m[2], m[3], m[4], m[5]
	if key != s.AccessKey {
		return "InvalidAccessKeyId"
	}
	var canonHeaders strings.Builder
	for _, h := range strings.Split(signed, ";") {
		v := r.Header.Get(h)
		if h == "host" {
			v = r.Host
		}
		if h == "content-length" {
			v = strconv.FormatInt(r.ContentLength, 10)
		}
		fmt.Fprintf(&canonHeaders, "%s:%s\n", h, strings.TrimSpace(v))
	}
	payload := r.Header.Get("X-Amz-Content-Sha256")
	if payload == "" {
		payload = "UNSIGNED-PAYLOAD"
	}
	canon := strings.Join([]string{r.Method, r.URL.EscapedPath(), canonicalQuery(r.URL.RawQuery),
		canonHeaders.String(), signed, payload}, "\n")
	canonSum := sha256.Sum256([]byte(canon))
	scope := date + "/" + region + "/s3/aws4_request"
	toSign := "AWS4-HMAC-SHA256\n" + r.Header.Get("X-Amz-Date") + "\n" + scope + "\n" + hex.EncodeToString(canonSum[:])
	k := hmacSHA256([]byte("AWS4"+s.Secret), date)
	for _, part := range []string{region, "s3", "aws4_request"} {
		k = hmacSHA256(k, part)
	}
	if hex.EncodeToString(hmacSHA256(k, toSign)) != got {
		return "SignatureDoesNotMatch"
	}
	return ""
}

func hmacSHA256(key []byte, data string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return h.Sum(nil)
}

func canonicalQuery(raw string) string {
	if raw == "" {
		return ""
	}
	parts := strings.Split(raw, "&")
	for i, p := range parts {
		if !strings.Contains(p, "=") {
			parts[i] = p + "="
		}
	}
	sort.Strings(parts)
	return strings.Join(parts, "&")
}
