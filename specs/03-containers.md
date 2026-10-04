# SPEC-03 — Containers

## Contexto

Gerenciamento de containers Docker (todos, não só apps). O backend fala com a Docker Engine API
pelo unix socket usando HTTP simples (sem SDK pesado).

## Requisitos

- R03.1 (M) `GET /api/v1/containers` lista todos os containers (inclusive parados): id curto, nome,
  imagem, estado (`running`, `exited`, ...), status textual, portas publicadas, criado em, e se é
  um app (`appId` quando tiver o label `omvhome.app`).
- R03.2 (M) `POST /api/v1/containers/{id}/start|stop|restart` executa a ação e retorna `204`.
- R03.3 (M) `DELETE /api/v1/containers/{id}?force=bool&volumes=bool` remove o container.
  Containers que pertencem a um app instalado só podem ser removidos via desinstalação do app
  (retorna `409 conflict` com mensagem explicando).
- R03.4 (M) `GET /api/v1/containers/{id}/logs?tail=200` retorna as últimas linhas (texto);
  `GET .../logs/stream` (SSE) acompanha em tempo real. O demultiplexador do stream do Docker
  (cabeçalho de 8 bytes) deve ser tratado quando o container não usa TTY.
- R03.5 (M) `GET /api/v1/containers/{id}` retorna detalhes: variáveis de ambiente (valores
  mascarados se o nome contiver `PASSWORD`, `SECRET`, `TOKEN`, `KEY`), mounts, redes, restart policy.
- R03.6 (S) `GET /api/v1/containers/{id}/stats` retorna CPU %, memória e I/O de rede (uma amostra).
- R03.7 (M) `POST /api/v1/containers/run` cria e inicia um container a partir de: imagem, nome
  opcional, portas, volumes, variáveis, restart policy. Imagem ausente é baixada (pull).
- R03.8 (M) `POST /api/v1/containers/import-run` aceita um comando `docker run ...` em texto e o
  converte para o formato de R03.7 (flags suportadas: `-d --name -p -v -e --restart --network
  -u --hostname --privileged`). Flags desconhecidas retornam `400` listando-as.
- R03.9 (M) `GET /api/v1/images` lista imagens; `POST /api/v1/images/pull` (SSE de progresso);
  `DELETE /api/v1/images/{id}`.
- R03.10 (M) Quando o socket do Docker estiver indisponível, as rotas devolvem `502 upstream_error`
  com mensagem "Docker indisponível"; `GET /api/v1/docker/status` informa versão ou erro.
- R03.11 (M) IDs de container e nomes recebidos pela API são validados (`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)
  antes de entrar na URL do Docker (evita injeção de caminho).

## Contrato (resumo)

```json
// GET /api/v1/containers
[{
  "id": "a1b2c3d4e5f6", "name": "jellyfin", "image": "jellyfin/jellyfin:latest",
  "state": "running", "status": "Up 2 hours",
  "ports": [{ "private": 8096, "public": 8096, "protocol": "tcp" }],
  "createdAt": "2026-10-04T10:00:00Z", "appId": "jellyfin"
}]
```

```json
// POST /api/v1/containers/run
{ "image": "nginx:alpine", "name": "web", "ports": [{ "host": 8081, "container": 80 }],
  "volumes": [{ "host": "/srv/data", "container": "/data", "readOnly": false }],
  "env": { "TZ": "America/Sao_Paulo" }, "restart": "unless-stopped" }
```

## Critérios de aceite

- A03.1 Contra um servidor Docker falso (httptest em unix socket/TCP), listar/start/stop/remover
  produzem as chamadas corretas.
- A03.2 O demux de logs reconstrói linhas de stdout/stderr de um stream multiplexado de fixture.
- A03.3 `import-run` converte `docker run -d --name x -p 8080:80 -v /a:/b -e A=1 --restart always img`.
- A03.4 ID inválido (`../etc`) retorna `400` sem chamar o Docker.
- A03.5 Remover container de app retorna `409`.
- A03.6 Env com `PASSWORD` aparece como `••••••••` em detalhes.
