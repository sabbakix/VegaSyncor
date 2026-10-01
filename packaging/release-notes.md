{{CHANGES}}

**VegaSyncor** è un servizio per Debian/Ubuntu che sincronizza a intervalli programmati cartelle di rete SMB/CIFS (PC e server Windows, server Linux con Samba) verso un server di backup. Si gestisce da una TUI utilizzabile anche via SSH.

## Installazione rapida (Debian / Ubuntu)

```bash
curl -fsSL https://github.com/sabbakix/VegaSyncor/releases/latest/download/vegasyncor-install.sh | sudo sh
```

Lo script:
- riconosce l'architettura (amd64 / arm64);
- scarica il pacchetto `.deb` e ne verifica il checksum;
- installa le dipendenze (`rsync`, `cifs-utils`, `smbclient`);
- avvia il servizio `vegasyncor`.

### Installazione manuale del pacchetto

```bash
# amd64 (PC/server x86_64). Per Raspberry Pi 4/5 e server ARM usare _arm64.deb
wget https://github.com/sabbakix/VegaSyncor/releases/download/v{{VERSION}}/vegasyncor_{{VERSION}}_amd64.deb
sudo apt install ./vegasyncor_{{VERSION}}_amd64.deb smbclient
```

### Senza pacchetto (altre distribuzioni con systemd)

```bash
tar xzf vegasyncor_{{VERSION}}_linux_amd64.tar.gz
cd vegasyncor_{{VERSION}}_linux_amd64
sudo ./install.sh ./vegasyncor
```

Su distribuzioni senza `apt` installare a mano `rsync`, `cifs-utils` e `smbclient`.

## Dopo l'installazione

```bash
sudo vegasyncor           # apre l'interfaccia di gestione
sudo vegasyncor status    # stato sintetico dei job
journalctl -u vegasyncor -f
```

1. Scheda **2 Connessioni**, tasto `n`: inserire indirizzo, utente e password del PC/server. Con `t` si prova l'accesso.
2. Scheda **1 Sincronizzazioni**, tasto `n`: scegliere sorgente, destinazione, modalità e pianificazione, poi `Ctrl+S` per salvare.
3. Tasto `s` per una **simulazione**, poi `l` per vedere cosa verrebbe copiato o cancellato.

> ⚠️ **Salvare una copia di `/etc/vegasyncor/master.key`**: senza la chiave le password salvate non sono più leggibili.

## Funzionalità

- **Sorgente in sola lettura garantita dal kernel**: viene montata con `mount.cifs -o ro`, quindi nessun file sulla sorgente può essere modificato o cancellato.
- **Password cifrate** (AES-256-GCM): non compaiono mai nei log, nei processi o nella TUI.
- **Modalità per ogni job**:
  - *Mirror + archivio*: i file cancellati o sovrascritti vengono conservati per N giorni in una cartella datata;
  - *Mirror*: la destinazione diventa una copia esatta;
  - *Solo aggiunte*: non cancella mai nulla.
- **Destinazione** locale oppure su una condivisione SMB.
- **Pianificazione**:
  - a intervalli, con fascia oraria e giorni facoltativi;
  - giornaliera, a uno o più orari;
  - in giorni scelti della settimana;
  - con espressione cron, oppure solo manuale.
- **Simulazione** (dry-run) con elenco dettagliato delle modifiche.
- **Blocco di sicurezza**: un mirror con sorgente vuota viene fermato per non svuotare il backup.
- Copia incrementale con `rsync`, limite di banda, esclusioni, storico e log di ogni esecuzione.

## File della release

| File | Descrizione |
|---|---|
| `vegasyncor-install.sh` | installazione/aggiornamento automatico |
| `vegasyncor_{{VERSION}}_amd64.deb` | pacchetto per Debian/Ubuntu x86_64 |
| `vegasyncor_{{VERSION}}_arm64.deb` | pacchetto per Debian/Ubuntu ARM64 |
| `vegasyncor_{{VERSION}}_linux_*.tar.gz` | binario statico, unità systemd e `install.sh` |
| `SHA256SUMS` | checksum (`sha256sum -c SHA256SUMS`) |

**Requisiti**: Debian 11+ / Ubuntu 20.04+ (o un'altra distribuzione con systemd), da eseguire come root.
