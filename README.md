# OMV Home

Interface web moderna, no estilo ZimaOS/CasaOS, para o **OpenMediaVault 7**. Ela roda **ao lado** da
interface original (que continua intacta na porta 80, para as configurações avançadas) e não altera o
sistema operacional nem arquivos do OMV.

- **Início**: widgets de CPU, memória, rede e discos, e os seus containers como **apps** (ícones clicáveis).
- **Loja**: instalação de apps em um clique a partir de templates docker compose (Jellyfin, Pi-hole,
  Vaultwarden, Syncthing, Uptime Kuma, qBittorrent, Home Assistant, Nextcloud) ou de um compose colado por você.
- **Containers**: containers e imagens Docker, logs ao vivo, criação por formulário ou colando um `docker run`.
- **Armazenamento**: discos e pastas compartilhadas (SMB) com um assistente de 3 passos.
- **Arquivos**: gerenciador de arquivos — *somente especificado* ([SPEC-07](specs/07-file-manager.md)).

Backend em **Go** (`backend/`), frontend em **React + TypeScript + Tailwind** (`frontend/`).
O desenvolvimento é guiado por specs: veja [`specs/`](specs/README.md).

## Como funciona

```
Navegador ──► nginx do OMV (:80, UI original, intocado)
          └─► omv-home (:8080)
                 ├─ API JSON-RPC do OMV (/rpc.php)  → discos, shares, usuários  (login = usuário admin do OMV)
                 └─ Docker (socket) + docker compose → containers e apps
```

O login usa as contas do OMV (só administradores). Toda alteração em shares passa pela API do OMV, então
aparece normalmente na interface original.

## Experimentar sem OMV (modo mock)

Requisitos: Go 1.23+ e Node 20+.

```bash
# terminal 1: API simulada (OMV + Docker de mentira) em :8080
cd backend && go run ./cmd/omv-home --mock --data-dir ./.devdata

# terminal 2: frontend com hot reload em :5173 (proxy para :8080)
cd frontend && npm install && npm run dev
```

Abra http://localhost:5173 e entre com `admin` / `admin` (conta existente só no modo mock; `user` / `user`
é recusado por não ser administrador).

## Build e instalação no OMV

```bash
scripts/build.sh                              # gera bin/omv-home (UI embutida)
GOOS=linux GOARCH=amd64 scripts/build.sh      # compilar para o servidor a partir de outro SO
sudo deploy/install.sh --binary bin/omv-home  # no servidor: serviço systemd omv-home
```

Depois abra `http://<ip-do-servidor>:8080`. Requisitos no servidor: OMV 7, **Docker** e **docker compose v2**
(plugin compose do omv-extras). `sudo deploy/install.sh --uninstall` remove o serviço e mantém os dados dos apps.

Também há uma imagem Docker (`deploy/Dockerfile`) e um exemplo em `deploy/docker-compose.yml`.

### Configuração

Variáveis de ambiente (arquivo `/etc/omv-home.env`, modelo em `deploy/omv-home.env.example`):

| Variável | Padrão | Descrição |
|----------|--------|-----------|
| `OMVHOME_LISTEN` | `:8080` | Endereço de escuta |
| `OMVHOME_OMV_URL` | `http://127.0.0.1` | URL do OMV (`/rpc.php`). **Inclua a porta se você mudou a do OMV** (ex.: `http://127.0.0.1:81`); o link "Configurações avançadas" usa a mesma porta |
| `OMVHOME_DOCKER_SOCKET` | `/var/run/docker.sock` | Socket do Docker |
| `OMVHOME_DATA_DIR` | `/var/lib/omv-home` | Projetos compose dos apps instalados |
| `OMVHOME_APPDATA_DIR` | `<DATA_DIR>/appdata` | Dados dos apps (use um disco do OMV) |
| `OMVHOME_PUID` / `OMVHOME_PGID` | `1000` / `100` | Usuário/grupo repassados aos apps |
| `OMVHOME_TZ` | fuso do sistema | Fuso horário dos apps |
| `OMVHOME_CATALOG_URL` | vazio | `index.json` (lista de URLs de templates) extra |
| `OMVHOME_MOCK` | `false` | Modo simulado |

## Segurança

- **O acesso ao socket do Docker equivale a root no servidor.** Por isso só administradores do OMV entram.
- O serviço não tem HTTPS próprio. Fora da rede local, coloque-o atrás de um proxy reverso com TLS.
- Cookie de sessão `HttpOnly` + `SameSite=Strict`, defesa CSRF por cabeçalho e limite de tentativas de login.
- Compose personalizado com `privileged`, rede host, `pid: host` ou acesso ao `docker.sock` exige confirmação explícita.

## Desenvolvimento

```bash
cd backend  && go test ./...        # testes do backend
cd frontend && npm test             # testes do frontend (vitest)
```

Cada funcionalidade nasce de uma spec em `specs/` (requisitos `R..` e critérios de aceite `A..`); os testes
referenciam os critérios. As partes que dependem do comportamento real do OMV ficam isoladas em
`backend/internal/omv` e estão marcadas com ⚠️ **a validar** (veja abaixo).

## Status e pontos a validar

O MVP está implementado e testado com OMV e Docker simulados. **Ainda não foi validado contra uma instância
OMV 7 real**: nomes de métodos RPC e de campos (`ShareMgmt`, `SMB`, `NFS`, `FileSystemMgmt`, `Config.applyChangesBg`,
`Exec.isRunning`, `UserMgmt`) foram escritos de memória e estão listados em `backend/internal/omv/*.go`.
O primeiro passo ao testar no servidor real é conferir esses métodos com `omv-rpc` e ajustar os adaptadores.

### Instalar em uma VM/servidor remoto com um comando

```bash
scripts/deploy-remote.sh usuario@IP-DO-OMV          # amd64 (PC, VirtualBox)
scripts/deploy-remote.sh usuario@IP-DO-OMV arm64    # Raspberry Pi e similares
```

Compila, copia por SSH e instala o serviço. Rode no Git Bash (Windows) ou em qualquer shell com `ssh`, Node e Go.
