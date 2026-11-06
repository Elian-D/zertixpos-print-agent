# ZertixPOS — Agente de impresión

Un solo `.exe` para Windows que se instala en una PC del negocio. Imprime los tickets de ZertixPOS directo en las impresoras térmicas, sin pasar por el navegador:

- **De red:** por TCP a `IP:9100`. Sin driver.
- **USB:** las conectadas a la PC del agente, por el spooler de Windows en modo RAW.

Cualquier equipo del negocio (otra PC, una tablet, un celular) imprime en ellas, porque el trabajo pasa por el servidor. La única condición es que la PC del agente esté encendida.

```
PC o celular del cajero ──(venta)──► ZertixPOS ──(Reverb: "hay un trabajo")──► Agente (esta PC)
                                        ▲                                          │
                                        └────────(REST: toma el trabajo, confirma)─┘
                                                                                   ▼
                                                            Impresora de red (IP:9100) o USB (spooler RAW)
```

El ticket (ESC/POS) lo arma ZertixPOS: el agente no sabe nada del negocio, solo recibe bytes y a qué impresora mandarlos. El diseño completo está en el spec de ZertixPOS, `docs/features/v1.5.6.md` §4.

---

## 1. Compilar

Hace falta **Go 1.25** o más nuevo. Se compila desde WSL o Linux; no hace falta Windows para compilar el `.exe`.

```bash
cd ~/zertixpos-print-agent
scripts/build.sh 0.1.0
```

El script corre `go vet` y `go test` y deja en **`dist/`**:

| Archivo | Para qué |
| :--- | :--- |
| `dist/zertixpos-agent.exe` | **El agente de Windows.** Es el que se publica y se instala en el negocio (~7 MB) |
| `dist/zertixpos-agent-linux` | El mismo agente para Linux: desarrollar y probar por IP desde WSL. Sin USB ni servicio |
| `dist/SHA256SUMS` | Las sumas de los dos |

La versión queda adentro del `.exe` (`-X main.version=0.1.0`) y la muestra `zertixpos-agent.exe version`. Compilado sin `scripts/build.sh` (`go build .`), dice `dev` y nunca se actualiza solo.

Para abrir el `.exe` desde Windows, la carpeta de WSL es `\\wsl.localhost\ubuntu-24.04\home\elian\zertixpos-print-agent\dist\`.

### Por qué compila sin ventana

El `.exe` se compila con `-H windowsgui`, así el servicio y el ícono de la bandeja no abren una consola negra. Los comandos que escriben algo (instalar, vincular, `status`, `test-print`) piden una consola cuando hace falta:

- Con doble clic se abre una ventana nueva, que espera un Enter antes de cerrarse.
- Desde una terminal se usa esa misma. **En PowerShell, agrega `| Out-Host` al final** para que espere a que termine y muestre la salida:

```powershell
.\zertixpos-agent.exe status | Out-Host
```

---

## 2. Probar por IP, sin servidor

Manda un ticket de prueba directo a la impresora: acentos en CP850, negrita, columnas, el QR opcional, la gaveta opcional y el corte. No hace falta estar vinculado.

```bash
# desde WSL
dist/zertixpos-agent-linux test-print --host 192.168.100.193 --qr https://zertixpos.com
```

```powershell
# desde Windows
.\zertixpos-agent.exe test-print --host 192.168.100.193 --qr https://zertixpos.com | Out-Host
.\zertixpos-agent.exe test-print --host 192.168.100.193 --drawer | Out-Host     # abre la gaveta
.\zertixpos-agent.exe test-print --host 192.168.100.193 --width 58 | Out-Host   # papel de 58 mm
```

Buscar impresoras en la red (puerto 9100 en la `/24` de la PC, con su MAC) y las instaladas en Windows:

```powershell
.\zertixpos-agent.exe scan | Out-Host
```

> Desde WSL `scan` no encuentra nada: WSL tiene su propia red (172.x) y no ve la del local. Desde WSL sí se llega a una IP conocida (`test-print --host`). Para buscar, usa el `.exe` en Windows.

---

## 3. Probar contra ZertixPOS en Sail (desarrollo)

ZertixPOS tiene que estar levantado con Reverb (`./vendor/bin/sail up -d`; el servicio `reverb` escucha en `localhost:8085`).

1. En `http://demo.localhost:8082` → Configuración → Impresoras → **Agregar equipo**: da un código de 6 dígitos.
2. Vincular el agente. `--server` es el dominio central de Sail, `http://localhost:8082`:

   ```bash
   dist/zertixpos-agent-linux pair 123456 --server http://localhost:8082
   ```
3. Correrlo en primer plano. El registro sale en pantalla:

   ```bash
   dist/zertixpos-agent-linux run
   ```

   Tiene que decir `timbre conectado (ws://localhost:8085)`.
4. En ZertixPOS: **Nueva impresora**, con el equipo recién vinculado y la IP `192.168.100.193`. Después **Imprimir prueba** en la fila. Sale en 1 o 2 segundos.

Cada máquina tiene su propia vinculación. Para tener varias, usa `ZERTIX_AGENT_DIR=/otra/carpeta`.

Lo mismo funciona con el `.exe` en Windows (`pair` y `run`). Los nombres `*.localhost` se resuelven a `127.0.0.1` aunque el sistema no lo haga.

---

## 4. Instalar en el negocio (Windows)

1. En ZertixPOS: Configuración → Impresoras → **Descargar el agente**. Baja `zertixpos-agent.exe` del release de GitHub.
2. **Doble clic** en el `.exe`.
   - Windows avisa "editor desconocido" (SmartScreen): **Más información → Ejecutar de todas formas**. El agente no tiene firma de código por ahora (decisión del 2026-11-05).
   - Pide permiso de administrador.
3. El instalador:
   1. Se copia a `C:\Program Files\ZertixPOS\Agente\zertixpos-agent.exe`.
   2. Pide el código de 6 dígitos (Configuración → Impresoras → Agregar equipo) y se vincula.
   3. Registra el servicio **"ZertixPOS - Agente de impresión"** (`ZertixPOSAgent`): arranca con la PC, aunque nadie inicie sesión, y Windows lo reinicia si se cae.
   4. Deja el ícono en la bandeja al iniciar sesión, para todos los usuarios.
4. En ZertixPOS: agregar las impresoras (a mano o desde **Encontradas por este equipo**) y elegir en cada terminal su impresora de ticket.

### El ícono de la bandeja

| Color del punto | Qué significa |
| :--- | :--- |
| Verde | Conectado: los tickets salen al instante |
| Ámbar | Conectado, pero el aviso instantáneo se está reconectando: los tickets salen en hasta 30 s |
| Gris | Sin vincular o sin conexión con ZertixPOS |
| Rojo | El servicio no está corriendo |

El menú trae el negocio y el equipo, el último ticket, **Imprimir prueba** (por impresora, sin pasar por el servidor), **Buscar impresoras en la red**, **Abrir ZertixPOS** y **Ver registro**. "Cerrar este ícono" solo cierra el ícono: el servicio sigue imprimiendo.

### Dónde guarda las cosas

| Qué | Dónde |
| :--- | :--- |
| El programa | `C:\Program Files\ZertixPOS\Agente\zertixpos-agent.exe` |
| La vinculación (negocio, token, WebSocket) | `C:\ProgramData\ZertixPOS\Agente\agent.json` |
| El registro | `C:\ProgramData\ZertixPOS\Agente\agent.log` (rota a `agent.log.1` al pasar 5 MB) |

En Linux, `~/.config/zertixpos-agent/`. `ZERTIX_AGENT_DIR` la cambia.

### Desinstalar

```powershell
& "C:\Program Files\ZertixPOS\Agente\zertixpos-agent.exe" uninstall | Out-Host          # deja la vinculación
& "C:\Program Files\ZertixPOS\Agente\zertixpos-agent.exe" uninstall --purge | Out-Host  # la borra también
```

El `.exe` se borra al reiniciar la PC: Windows no deja borrar un programa en uso. Para que deje de imprimir sin desinstalar, se **desvincula** desde ZertixPOS: el token deja de servir enseguida.

---

## 5. Impresoras USB

El agente manda los bytes al spooler de Windows en modo RAW: el driver no los toca, así que llega el ESC/POS que armó ZertixPOS. Lo único que hace falta es que la impresora esté instalada en Windows **con su driver**.

1. Instalar el driver de la impresora (el del fabricante, o el genérico "Generic / Text Only" si el fabricante no tiene).
2. Ver el nombre exacto en **Configuración de Windows → Bluetooth y dispositivos → Impresoras y escáneres**, o con `zertixpos-agent.exe scan`.
3. Probar: `.\zertixpos-agent.exe test-print --usb "POS-80C" | Out-Host`.
4. En ZertixPOS: Nueva impresora → **USB** → ese nombre (o "Agregar" en las encontradas por el equipo).

Una impresora USB solo la atiende el agente de la PC donde está conectada. Una USB en otra PC necesita su propio agente en esa PC, o un adaptador USB a red (US$20–40) que le da una IP.

La 2connet 2C-POS80-01-V7 es VID `0x2CB7`, PID `0x811B` (spec §4, "La impresora de prueba").

---

## 6. Publicar una versión

Los negocios descargan el agente del **release de GitHub** de este repositorio. Por eso el repositorio es público: aquí no hay nada del negocio, y **nunca se sube un token, un `agent.json` ni un `.env`**.

1. Compilar con la versión nueva: `scripts/build.sh 0.2.0`.
2. Crear el release `v0.2.0` y adjuntar **`dist/zertixpos-agent.exe` con ese nombre exacto**. La URL queda así:
   `https://github.com/Elian-D/zertixpos-print-agent/releases/download/v0.2.0/zertixpos-agent.exe`

   ```bash
   gh release create v0.2.0 dist/zertixpos-agent.exe dist/SHA256SUMS --title "v0.2.0" --notes "..."
   ```
3. En el VPS de ZertixPOS: `PRINT_AGENT_VERSION=0.2.0` en el `.env` y `sudo deploy/restart.sh`.

Los agentes se actualizan solos. En el siguiente aviso de vida comparan su versión con `PRINT_AGENT_VERSION`; si la publicada es más nueva, bajan el `.exe`. Esto se revisa cada 6 horas como máximo. El agente comprueba que lo bajado sea un ejecutable, deja el actual como `.old` y se reinicia como servicio. Solo el servicio de Windows se actualiza; `run` y Linux no.

`PRINT_AGENT_DOWNLOAD_URL` (opcional, con `{version}`) cambia de dónde se baja.

---

## 7. Cómo habla con ZertixPOS

Todo va por HTTPS al subdominio del negocio, con `Authorization: Bearer <token>`, salvo el primer paso de la vinculación.

| Paso | Llamada | Para qué |
| :--- | :--- | :--- |
| 1 | `POST https://zertixpos.com/api/print-agent/locate` `{code}` | De qué negocio es el código: `{server_url}` |
| 2 | `POST <negocio>/api/print-agent/pair` `{code, hostname, version}` | Canjea el código por el token, una sola vez |
| Cada 30 s | `POST /api/print-agent/heartbeat` | "Estoy vivo": impresoras, trabajos pendientes, versión publicada |
| Timbre | WebSocket `wss://<negocio>/ws/app/<key>` (Pusher, protocolo 7), canal `private-tenant.<id>.print-agent.<id>` | Evento `print.job`: hay un trabajo |
| Firma | `POST /api/print-agent/broadcasting/auth` `{socket_id, channel_name}` | La firma para suscribirse al canal (solo el propio) |
| Trabajos | `GET /api/print-agent/jobs` → `POST /jobs/{id}/claim` → `POST /jobs/{id}/complete` `{ok, error}` | Pedir, tomar (devuelve el ESC/POS en base64 y la impresora) y confirmar |
| Búsqueda | `POST /api/print-agent/discovered` `{printers: [...]}` | Lo encontrado en la red y en Windows. Una impresora con la misma MAC y otra IP se actualiza sola |

- **Un trabajo nunca se pierde por un corte del WebSocket:** el aviso de vida trae cuántos esperan, y al reconectar el agente pide los pendientes.
- **Si la impresora de red no responde en su IP,** el agente la busca por su MAC en la red: el router le pudo cambiar la IP.
- **Un trabajo que falla** vuelve a la fila hasta 3 intentos. Uno que nadie tomó en 10 minutos vence: un ticket que sale media hora tarde confunde.
- **Un `401`** quiere decir que el equipo se desvinculó. El agente borra su token y espera un código nuevo.

Para la bandeja, el servicio atiende solo en `127.0.0.1:17890` (`GET /status`, `POST /test?printer=ID`, `POST /scan`).

---

## 8. Estructura

```
main.go                     los comandos (install, pair, run, service, tray, status, test-print, scan…)
internal/agent              el agente en marcha: aviso de vida, timbre, trabajos, búsqueda, estado local
internal/api                el cliente REST de ZertixPOS
internal/pusher             el cliente del protocolo de Pusher (Reverb)
internal/printer            TCP 9100 y el spooler de Windows (RAW)
internal/discovery          barrido del 9100, MAC por ARP, impresoras de Windows
internal/escpos             el ticket de prueba local
internal/update             la actualización automática
internal/winsvc             el servicio de Windows
internal/install            vincular, instalar y desinstalar
internal/tray, internal/icon   el ícono de la bandeja (dibujado en código, sin archivos)
internal/console            la consola de los comandos con un .exe sin ventana
internal/config             agent.json
```

```bash
go test ./...                           # pruebas
GOOS=windows GOARCH=amd64 go vet ./...  # lo de Windows también se revisa desde Linux
```
