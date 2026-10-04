# SPEC-05 — App Store

## Contexto

Catálogo de templates compose que o usuário instala com um clique. Fonte: catálogo embutido no
binário + índice remoto opcional (`OMVHOME_CATALOG_URL`). Compatibilidade com o catálogo do
CasaOS/ZimaOS (`x-casaos`) é um objetivo posterior; o MVP define o próprio formato mínimo.

## Formato do template

Um arquivo `docker-compose.yml` com extensão `x-omvhome` no nível raiz:

```yaml
name: jellyfin
x-omvhome:
  id: jellyfin
  title: Jellyfin
  tagline: Servidor de mídia
  description: Streaming dos seus filmes, séries e músicas.
  icon: https://.../jellyfin.png      # ou emoji:🎬
  category: Media
  port: 8096                          # porta do host exibida como "Abrir"
  main: jellyfin                      # serviço principal (recebe os labels)
  env:                                # campos pedidos no formulário de instalação
    - { key: MEDIA_DIR, label: Pasta de mídia, type: path, default: /srv/media, required: true }
services:
  jellyfin:
    image: jellyfin/jellyfin:latest
    ports: ["${PORT:-8096}:8096"]
    volumes:
      - ${APPDATA}/config:/config
      - ${MEDIA_DIR}:/media
    restart: unless-stopped
```

Variáveis reservadas, preenchidas pelo sistema: `APPDATA` (= `APPDATA_DIR/<id>`), `PORT` (porta do host
escolhida na instalação; templates devem usar `${PORT:-<padrão>}`), `PUID` (padrão 1000), `PGID` (padrão 100,
grupo `users` do OMV), configuráveis por `OMVHOME_PUID/PGID/TZ`,
`TZ`. Tipos de campo: `string`, `path`, `port`, `password`, `bool`, `select` (com `options`).

## Requisitos

- R05.1 (M) `GET /api/v1/store/apps?q=&category=` lista o catálogo: id, título, tagline, ícone,
  categoria, e `installed` (bool).
- R05.2 (M) `GET /api/v1/store/apps/{id}` retorna o template completo (descrição, campos,
  compose bruto) para a tela de detalhes.
- R05.3 (M) `POST /api/v1/store/apps/{id}/install` com `{ "env": { ... }, "port": 8096 }`:
  valida campos, grava `DATA_DIR/apps/<id>/docker-compose.yml` e `.env`, cria os diretórios de
  `APPDATA`, injeta os labels `omvhome.*` no serviço principal e executa
  `docker compose -p omvhome-<id> up -d`. Responde `202` e o progresso vai por
  `GET /api/v1/store/installs/{id}/events` (SSE: `pull`, `starting`, `done`, `error`).
- R05.4 (M) Instalar app já instalado retorna `409 conflict`.
- R05.5 (M) Conflito de porta: antes de instalar, verificar se a porta do host está em uso por
  outro container (via Docker) ou escutando no host; retornar `409` com a porta e quem a usa.
- R05.6 (M) Campos `path` devem ser absolutos, sem `..`; o diretório é criado se não existir.
- R05.7 (M) `POST /api/v1/store/custom` instala um compose colado pelo usuário: `{ "name": "...",
  "compose": "yaml", "mainService": "...", "port": 8080 }`. O sistema valida o YAML, rejeita
  `privileged: true`, `network_mode: host`, `pid: host` e montagens de `docker.sock` ou `/` sem
  `allowUnsafe: true`, e injeta labels.
- R05.8 (S) `OMVHOME_CATALOG_URL` aponta para um `index.json` (lista de templates); falha de
  rede não derruba o catálogo embutido, apenas registra aviso.
- R05.9 (M) Frontend, **Loja**: grade de cards com busca e categorias; página de detalhes com
  descrição e formulário gerado a partir de `env`; botão "Instalar" com barra de progresso/log;
  ao concluir, redireciona à home destacando o novo app.
- R05.10 (M) O catálogo embutido do MVP inclui no mínimo: Jellyfin, Pi-hole, Vaultwarden,
  Syncthing, Uptime Kuma, qBittorrent, Home Assistant, Nextcloud (simples, SQLite).

## Critérios de aceite

- A05.1 Parse de template válido e rejeição de template sem `x-omvhome.id`/`main`.
- A05.2 Render: `${APPDATA}` e `${MEDIA_DIR}` substituídos no `.env`; labels presentes no
  serviço principal e ausentes nos demais.
- A05.3 Instalar duas vezes o mesmo app → segunda retorna 409.
- A05.4 Porta ocupada → 409 antes de qualquer escrita em disco.
- A05.5 Caminho com `..` rejeitado com 400.
- A05.6 Compose custom com `privileged: true` rejeitado sem `allowUnsafe`.
- A05.7 O comando compose é montado com argumentos como lista (sem shell) — teste com executor falso.
