# SPEC-08 — Empacotamento e deploy

## Requisitos

- R08.1 (M) Um único binário Go com o frontend embutido (`go:embed`), sem dependência de Node em
  produção.
- R08.2 (M) Unidade systemd `omv-home.service` (`deploy/omv-home.service`) com `After=docker.service`,
  `Restart=on-failure` e `EnvironmentFile=-/etc/omv-home.env`.
- R08.3 (M) Script `deploy/install.sh` para Debian 12/OMV 7: instala o binário em
  `/usr/local/bin`, cria `/var/lib/omv-home`, habilita o serviço. Não altera nginx nem arquivos do
  OMV. Idempotente; `--uninstall` remove tudo, exceto os dados dos apps.
- R08.4 (M) `deploy/Dockerfile` multi-stage (build do frontend, build do Go, imagem final
  distroless/alpine) e `deploy/docker-compose.yml` de exemplo montando o socket do Docker e
  `/var/lib/omv-home`.
- R08.5 (S) Pacote `.deb` (fase posterior) via `nfpm`.
- R08.6 (M) `GET /healthz` sem autenticação para o monitoramento.
- R08.7 (M) O serviço roda o `docker compose` v2 do host; se ausente, `GET /api/v1/docker/status`
  indica `composeAvailable: false` e a loja desabilita a instalação com mensagem clara.
- R08.8 (M) Documentar no README: portas, variáveis, HTTPS (reverse proxy), e o alerta sobre o
  acesso ao socket do Docker.

## Critérios de aceite

- A08.1 `go build` produz um binário que serve a UI e a API sem arquivos externos.
- A08.2 `docker build` conclui e o container responde em `/healthz`.
- A08.3 `install.sh --uninstall` deixa `/var/lib/omv-home/appdata` intacto.
