# SPEC-00 — Visão geral e arquitetura

## Objetivo

Uma interface web moderna, no estilo ZimaOS/CasaOS, que roda **ao lado** do OpenMediaVault 7
(Debian 12). Foco: simplicidade para o usuário doméstico, containers expostos como "apps".
A UI original do OMV permanece intacta para configurações avançadas.

## Não objetivos

- Não modificar o sistema operacional, o nginx do OMV nem os arquivos de configuração do OMV.
- Não substituir a UI original. Funcionalidades avançadas (RAID, rede detalhada, plugins) ficam lá.
- Não ter banco de dados próprio para dados que o OMV já guarda (usuários, shares, filesystems).

## Princípios

1. **OMV é a fonte da verdade** para usuários, discos, shares e serviços. Toda alteração nesses
   dados passa pela API JSON-RPC do OMV (`/rpc.php`), nunca por edição direta de arquivos.
2. **Sessão por usuário**: a nova UI não guarda credenciais de administrador. Ela repassa o login
   do usuário ao OMV e mantém a sessão OMV dele no servidor.
3. **Docker é a fonte da verdade** para containers. Apps são containers com labels `omvhome.*`.
4. **Isolamento da camada OMV**: todo conhecimento sobre o RPC do OMV fica em
   `backend/internal/omv`, atrás de uma interface, para absorver mudanças entre versões.
5. **Modo mock**: `--mock` sobe o backend com OMV e Docker simulados, permitindo desenvolver e
   demonstrar a UI sem uma máquina OMV.

## Arquitetura

```
Navegador ──► nginx do OMV (:80, UI original, intocado)
          └─► omv-home (:8080)  ── Go: API REST + estáticos do React (embed)
                 ├─ internal/omv      cliente JSON-RPC do OMV (sessão por usuário)
                 ├─ internal/docker   cliente da Docker Engine API (unix socket)
                 ├─ internal/compose  execução de `docker compose` + render de templates
                 ├─ internal/catalog  catálogo de apps (embutido + remoto opcional)
                 ├─ internal/system   métricas do host (/proc, statfs)
                 └─ internal/auth     sessões, cookies, middleware
```

### Stack

| Camada | Escolha |
|--------|---------|
| Backend | Go 1.22+, `net/http` (ServeMux com métodos/padrões), `log/slog`, `yaml.v3` |
| Frontend | React 18 + TypeScript, Vite, Tailwind CSS, React Router, TanStack Query |
| Contrato | REST JSON em `/api/v1`, erros em formato único (abaixo) |
| Tempo real | Server-Sent Events para métricas e logs (sem WebSocket no MVP) |

### Formato de erro

```json
{ "error": { "code": "not_found", "message": "container não encontrado" } }
```

Códigos: `unauthorized`, `forbidden`, `not_found`, `invalid_request`, `conflict`,
`upstream_error` (OMV/Docker falhou), `internal`.

### Configuração (variáveis de ambiente / flags)

| Variável | Padrão | Descrição |
|----------|--------|-----------|
| `OMVHOME_LISTEN` | `:8080` | Endereço de escuta |
| `OMVHOME_OMV_URL` | `http://127.0.0.1` | URL base do OMV (`/rpc.php`) |
| `OMVHOME_DOCKER_SOCKET` | `/var/run/docker.sock` | Socket do Docker |
| `OMVHOME_DATA_DIR` | `/var/lib/omv-home` | Estado do app (compose files de apps instalados) |
| `OMVHOME_APPDATA_DIR` | `<DATA_DIR>/appdata` | Raiz dos volumes dos apps (pode ser um share do OMV) |
| `OMVHOME_CATALOG_URL` | vazio | Índice remoto opcional de apps |
| `OMVHOME_MOCK` | `false` | Liga o modo mock |

## Segurança (transversal)

- R00.1 (M) Cookie de sessão `HttpOnly`, `SameSite=Strict`, `Secure` quando atrás de HTTPS.
- R00.2 (M) Todas as rotas `/api/v1/*` exceto `POST /auth/login` exigem sessão válida.
- R00.3 (M) Mutações (POST/PUT/PATCH/DELETE) exigem o header `X-Requested-With: omv-home`
  (defesa CSRF simples, somada a `SameSite=Strict`).
- R00.4 (M) Apenas usuários com papel de administrador do OMV podem entrar no MVP.
- R00.5 (M) Acesso ao socket do Docker equivale a root: o serviço escuta apenas na rede local por
  padrão e a documentação deve alertar contra expor sem HTTPS.
- R00.6 (S) Rate limit no login: 5 falhas por IP em 5 minutos bloqueiam por 5 minutos.
- R00.7 (M) Nenhum segredo (senha, cookie OMV) é gravado em log.

## Estrutura do repositório

```
specs/      especificações
backend/    módulo Go (cmd/omv-home, internal/*)
frontend/   app React (Vite)
deploy/     Dockerfile, unit systemd, scripts de pacote
```
