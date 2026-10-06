# SPEC-09 — Fontes de apps personalizadas

## Contexto

Além do catálogo embutido (SPEC-05), o usuário pode adicionar **fontes**: repositórios de terceiros no
formato de app store do **ZimaOS/CasaOS** ou do **Umbrel**. Exemplos reais:

- ZimaOS/CasaOS: `https://github.com/WisdomSky/LinuxServer-AppStore`
- Umbrel: `https://github.com/dennysubke/dennys-umbrel-app-store`

Cada app da fonte é convertido, em memória, para o formato de template do SPEC-05 e instalado pelo
mesmo fluxo (compose em `DATA_DIR/apps/<id>`). O OMV Home **não** reproduz o runtime do ZimaOS nem do
Umbrel; apps que dependem dele são listados como incompatíveis, com o motivo.

## Obtenção do repositório

- A URL aceita é `https://github.com/<dono>/<repo>` (também `.git`, barra final ou
  `/tree/<branch>`) ou qualquer URL `http(s)` terminada em `.zip`.
- Repositórios do GitHub são baixados como zip: `https://github.com/<dono>/<repo>/archive/HEAD.zip`
  (ou `/archive/refs/heads/<branch>.zip`). Não é necessário git instalado.
- O zip é guardado em `DATA_DIR/sources/<id>.zip` e a lista de fontes em `DATA_DIR/sources/sources.json`,
  de modo que a loja funciona offline após a primeira obtenção.
- Limites: zip de até 100 MB, 1 MB por arquivo lido, até 3000 apps por fonte. Somente `.yml`, `.yaml` e
  `.json` relevantes são lidos; nada do zip é extraído para o disco.

## Detecção de formato

| Formato | Marcador | Layout de cada app |
|---|---|---|
| Umbrel | `umbrel-app-store.yml` na raiz (`id`, `name`) | `<pasta>/umbrel-app.yml` + `<pasta>/docker-compose.yml` |
| ZimaOS/CasaOS | pasta `Apps/` na raiz | `Apps/<Nome>/docker-compose.yml` com `x-casaos` na raiz |

Se nenhum marcador existir, a fonte é rejeitada (`400`).

## Conversão ZimaOS/CasaOS

- `id` = `name` do compose (ou o nome da pasta), em minúsculas, até 40 caracteres.
- Título, tagline e descrição: `x-casaos.title/tagline/description` por idioma (preferência
  `pt_BR`, `en_US`, primeiro disponível). `icon`, `category`, `index` (→ caminho de abertura),
  `scheme`, `main` (serviço principal; se ausente e houver um único serviço, ele).
- Porta de acesso: `x-casaos.port_map`; no serviço principal a porta publicada igual a ela vira
  `${PORT:-<porta>}` (o usuário pode trocá-la na instalação).
- Volumes de bind em `/DATA/AppData/<app>/…` viram `${APPDATA}/…`. Qualquer outro `/DATA/…` vira um
  campo `path` obrigatório no formulário de instalação.
- Variáveis: `$PUID`, `$PGID`, `$TZ` são preenchidas pelo sistema; `$AppID` vira o id; `$WEBUI_PORT`
  vira `${PORT:-<porta>}`; qualquer outra variável sem valor → app incompatível.
- `x-casaos` dos serviços é descartado. Se `architectures` não contiver a arquitetura do servidor
  → incompatível.

## Conversão Umbrel

- Metadados de `umbrel-app.yml`: `id`, `name`, `tagline`, `description`, `icon`, `category`, `port`,
  `path`, `defaultUsername`/`defaultPassword` (acrescentados à descrição).
- O serviço `app_proxy` é removido. O serviço principal e a porta interna vêm de `APP_HOST`
  (`<id>_<serviço>_1`) e `APP_PORT`; a porta é publicada como `${PORT:-<port do manifesto>}:<APP_PORT>`.
  Como alguns manifestos apontam o proxy para a porta errada (ex.: Portainer aponta para 8000, o túnel de agentes,
  enquanto a interface fica em 9000), o lado do container é um campo do formulário (`CONTAINER_PORT`, padrão
  `APP_PORT`, marcado `internal`: validado, mas não checado contra portas do host).
- Referências `<id>_<serviço>_1` viram o nome do serviço (rede interna do compose).
- Variáveis: `APP_DATA_DIR` → `${APPDATA}`; `APP_ID` → id; `APP_SEED` → segredo aleatório gerado na
  instalação (gravado no `.env` do app); `APP_DOMAIN` e `DEVICE_DOMAIN_NAME` → campo opcional
  "Endereço do servidor". Qualquer outra (`UMBREL_ROOT`, `APP_*_IP`, `TOR_PROXY_*`, `APP_PASSWORD`…)
  ou `dependencies` não vazio → incompatível.

## Segurança

- Conteúdo de fontes é **não confiável**. Passa pela mesma checagem do compose personalizado
  (SPEC-05 R05.7): `privileged`, `network_mode: host`, `pid: host`, montagem de `docker.sock` ou `/`
  marcam o app como **inseguro**; a instalação exige `allowUnsafe: true` (checkbox explícito na UI).
- Avisos (não bloqueiam): `cap_add`, `devices`, `security_opt`, `ipc: host`.
- O compose convertido é sempre exibido na página do app antes de instalar.
- Somente `http`/`https`. Endpoints de fontes exigem sessão e CSRF como os demais. O servidor aceita
  URLs de rede local (fontes hospedadas em casa), por isso só administradores as gerenciam.

## Requisitos

- R09.1 (M) `GET /api/v1/store/sources` lista as fontes: `id`, `name`, `url`, `format`
  (`umbrel`|`casaos`), `apps` (quantidade), `updatedAt`, `error`.
- R09.2 (M) `POST /api/v1/store/sources` `{ "url": "…", "name": "…"? }` baixa, detecta o formato,
  converte e persiste. `400` para URL/formato inválido, `409` para URL já cadastrada, `502` se o
  download falhar. Responde `201` com a fonte.
- R09.3 (M) `DELETE /api/v1/store/sources/{id}` remove a fonte e o zip em cache (`204`). Apps já
  instalados continuam funcionando.
- R09.4 (M) `POST /api/v1/store/sources/{id}/refresh` baixa de novo; se falhar, mantém o cache
  anterior e responde `502`.
- R09.5 (M) `GET /store/apps` aceita `?source=<id>` (`builtin` = embutidos) e cada card traz
  `source` e `unsupported` (bool); o detalhe traz `source`, `unsupported` (motivo), `unsafe` (lista) e
  `warnings`.
- R09.6 (M) Instalar app incompatível → `400`; app inseguro sem `allowUnsafe` → `400` com a lista.
  Demais regras de instalação do SPEC-05 valem sem alteração.
- R09.7 (M) Colisão de ids: embutidos vencem; entre fontes, o app da fonte mais nova recebe o sufixo
  `-<id da fonte>` (truncado em 40).
- R09.8 (S) Ao iniciar, fontes com mais de 24 h são atualizadas em segundo plano; falhas só geram aviso.
- R09.9 (M) Frontend, **Loja**: filtro por fonte; botão "Fontes" abre o gerenciador (listar, adicionar por
  URL, atualizar, remover); cards de fonte externa mostram o nome da fonte e "Incompatível" quando for
  o caso. Página do app: avisos, motivo da incompatibilidade (botão desabilitado) e checkbox para apps
  inseguros.
- R09.11 (M) Bind mounts sobre diretórios da imagem escondem o conteúdo dela (um volume nomeado o copiaria na primeira
  vez, um bind mount não). Para apps de fontes externas, antes do `compose up` cada pasta de bind em `APPDATA` que
  esteja vazia recebe o conteúdo que a imagem tem no caminho montado (`docker create` + `docker cp -a`), e só depois
  ocorre o ajuste de dono de R09.10. Apps embutidos não são afetados.
- R09.12 (M) Apps de fontes externas cujo serviço não define `user:` rodam como o `USER` da imagem (ex.: `filebrowser`,
  UID 1000). As pastas de bind em `APPDATA` desse serviço recebem `chown` para esse usuário (UID numérico, ou nome
  resolvido com `id` dentro da imagem); imagens que rodam como root não mudam nada. Vale `user:` do compose quando houver.
- R09.10 (M) Antes do `compose up`, para cada serviço com `user:` numérico (`1000`, `1000:100`), as pastas
  de bind dentro de `APPDATA` usadas por ele são criadas e recebem `chown` para esse uid:gid. Assim o Docker não
  as cria como root e o app consegue gravar. Nomes de usuário, caminhos fora de `APPDATA` e arquivos
  (origem com extensão) são ignorados.

## Critérios de aceite

- A09.1 Zip de loja Umbrel de exemplo → apps convertidos sem `app_proxy`, com porta publicada, `${APPDATA}`
  no lugar de `APP_DATA_DIR` e hostnames `<id>_<svc>_1` reescritos.
- A09.2 Zip de loja CasaOS de exemplo → `/DATA/AppData/x` vira `${APPDATA}`, porta de `port_map`
  vira `${PORT:-…}`, título em `pt_BR`/`en_US`.
- A09.3 App com variável desconhecida ou `dependencies` → incompatível com motivo.
- A09.4 App com `privileged: true` → `unsafe`; instalar sem `allowUnsafe` → 400.
- A09.5 Adicionar fonte (servidor HTTP falso) persiste zip e metadados; reiniciar o catálogo recarrega
  sem rede; remover apaga ambos.
- A09.6 URL com esquema inválido ou sem marcador de formato → 400.
- A09.7 Falha de refresh mantém o catálogo anterior.
- A09.8 UI: adicionar fonte mostra-a na lista; app incompatível tem botão desabilitado.
- A09.10 App de fonte com bind mount `${APPDATA}/…` sobre um diretório que a imagem preenche (ex.: FileBrowser Quantum
  em `/home/filebrowser/data`): se a pasta do host está vazia, o conteúdo da imagem é copiado para ela antes do
  `compose up`; pasta já preenchida, arquivos, caminhos que a imagem não tem e falha na cópia não bloqueiam nada.
- A09.11 Serviço sem `user:` cuja imagem tem `USER 1000`: as pastas de bind em `${APPDATA}` ficam com dono 1000, e uma
  imagem root (ou desconhecida) não gera `chown`.
- A09.9 Serviço com `user: <uid>[:<gid>]` numérico e volume em `${APPDATA}/…`: a pasta é criada antes do
  `compose up` e entregue (recursivamente) a esse uid:gid; serviços sem `user` numérico não são tocados;
  falha de `chown` só gera aviso.

## Notas de validação

- Conversão exercitada com os repositórios reais citados acima: 246 apps do `LinuxServer-AppStore`
  (todos instaláveis; 3 marcados como inseguros por `docker.sock`/montagem de `/`) e 328 do
  `dennys-umbrel-app-store` (290 instaláveis; os demais incompatíveis por dependências, variáveis do Umbrel
  sem equivalente ou serviço principal indeterminado).
- Ainda não testado em Docker real: a execução de apps de terceiros. O `chown` exige que o serviço rode como
  root (o systemd unit do pacote roda); sem root ele só avisa no log.
- O app_proxy do Umbrel (autenticação/HTTPS) não existe aqui: o app é publicado direto na porta escolhida.
