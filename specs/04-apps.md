# SPEC-04 — Apps (home estilo ZimaOS)

## Conceito

**App** = projeto docker compose instalado e gerenciado pela nova UI, ou qualquer container que
carregue os labels abaixo. A home mostra cada app como um ícone clicável.

### Labels (no serviço principal do compose)

| Label | Exemplo | Uso |
|-------|---------|-----|
| `omvhome.app` | `jellyfin` | ID do app (obrigatório para ser app) |
| `omvhome.name` | `Jellyfin` | Nome exibido |
| `omvhome.icon` | URL ou `emoji:🎬` | Ícone |
| `omvhome.port` | `8096` | Porta web (do host) usada para "Abrir" |
| `omvhome.scheme` | `http` | `http` ou `https` (padrão `http`) |
| `omvhome.path` | `/web` | Caminho inicial opcional |
| `omvhome.category` | `Media` | Agrupamento |

## Requisitos

- R04.1 (M) `GET /api/v1/apps` lista apps: id, nome, ícone, categoria, estado agregado
  (`running` se o serviço principal roda; `stopped`; `partial`), URL de abertura e se foi
  instalado pela loja (`managed`).
- R04.2 (M) A URL de abertura usa o host com que o usuário acessa a UI (`Host` do request,
  sem a porta) + `omvhome.port`. A montagem acontece no **frontend**, o backend entrega
  `port`, `scheme` e `path`.
- R04.3 (M) `POST /api/v1/apps/{id}/start|stop|restart` age sobre todos os containers do app
  (`docker compose` para apps gerenciados; containers individuais para apps só com labels).
- R04.4 (M) `POST /api/v1/apps/{id}/update` faz `compose pull` + `up -d` (apps gerenciados).
- R04.5 (M) `DELETE /api/v1/apps/{id}?purgeData=bool` faz `compose down` e remove o projeto de
  `DATA_DIR/apps/<id>`; com `purgeData=true` também apaga `APPDATA_DIR/<id>`. Apagar dados
  exige confirmação explícita na UI (digitar o nome do app).
- R04.6 (M) `GET /api/v1/apps/{id}/logs/stream` (SSE) agrega logs do serviço principal.
- R04.7 (M) Frontend, **Home**: grade de cards com ícone, nome, status (ponto colorido + texto),
  clique abre o app em nova aba; menu do card com iniciar/parar/reiniciar/logs/atualizar/remover.
  App parado aparece esmaecido e o clique oferece "Iniciar".
- R04.8 (M) Frontend: busca por nome e filtro por categoria na home.
- R04.9 (S) Containers sem labels não aparecem na home, mas ficam em "Containers" com ação
  "Transformar em app" (define labels via recriação — fora do MVP; ver Roadmap).
- R04.10 (M) Ícone ausente ou com falha de carga cai em um avatar com a inicial do nome.

## Critérios de aceite

- A04.1 Containers com labels `omvhome.*` são agrupados em apps corretamente (teste de unidade
  sobre a listagem do Docker).
- A04.2 Estado agregado: todos rodando → `running`; nenhum → `stopped`; misto → `partial`.
- A04.3 Parar um app gerenciado invoca `docker compose -p omvhome-<id> stop`.
- A04.4 `purgeData` sem confirmação da UI não é possível; o backend valida que o caminho a apagar
  está dentro de `APPDATA_DIR` e não contém `..`.
- A04.5 A grade da home renderiza os apps do mock e filtra por busca.
