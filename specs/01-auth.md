# SPEC-01 — Autenticação

## Contexto

Usuários são os do OMV. A nova UI faz login no OMV em nome do usuário e mantém a sessão OMV
(cookie `PHPSESSID`) no servidor, associada a um token de sessão próprio entregue ao navegador.

## Requisitos

- R01.1 (M) Quando o usuário envia usuário e senha em `POST /api/v1/auth/login`, o sistema deve
  chamar `Session.login` do OMV e, se autenticado, criar uma sessão local.
- R01.2 (M) Se o OMV recusar as credenciais, retornar `401 unauthorized` sem detalhar o motivo.
- R01.3 (M) Se o usuário autenticar mas não for administrador, retornar `403 forbidden`
  e descartar a sessão OMV criada (chamar `Session.logout`).
- R01.4 (M) A sessão local deve ser um token aleatório de 256 bits em cookie `omvhome_session`,
  `HttpOnly`, `SameSite=Strict`, sem o valor de senha em nenhum lugar.
- R01.5 (M) `POST /api/v1/auth/logout` deve encerrar a sessão local e chamar `Session.logout` no OMV.
- R01.6 (M) `GET /api/v1/auth/me` retorna o usuário logado ou `401`.
- R01.7 (M) Sessões expiram após 12 h sem uso (deslizante) e no máximo 7 dias absolutos.
- R01.8 (M) Quando uma chamada ao OMV com a sessão do usuário retornar sessão expirada, o sistema
  deve invalidar a sessão local e responder `401`, para a UI voltar ao login.
- R01.9 (S) Rate limit de login (ver R00.6).
- R01.10 (M) Sessões ficam em memória; reiniciar o serviço exige novo login (aceitável no MVP).
- R01.11 (M) O OMV expira a própria sessão por inatividade (padrão: poucos minutos), mesmo com a sessão local ainda
  válida. Enquanto o usuário estiver ativo (requisição nos últimos 10 min), o sistema faz uma chamada leve ao OMV
  (`Config.isDirty`) a cada 60 s para manter a sessão do OMV viva; sem atividade, ela expira normalmente.
- R01.12 (M) Sessão do OMV expirada é reconhecida pelos códigos 5000/5001, por mensagens como "Session not
  authenticated/expired/timeout" e por HTTP 401/403 de `/rpc.php`: a sessão local é descartada e a API responde 401
  (a UI volta ao login), nunca um 502 genérico. Se o keep-alive detectar a expiração, a sessão local também é descartada.

## Contrato

`POST /api/v1/auth/login`
```json
{ "username": "admin", "password": "••••" }
```
→ `200` `{ "user": { "username": "admin", "role": "admin" } }` + `Set-Cookie`.

`GET /api/v1/auth/me` → `200` `{ "user": { ... } }` · `401`.
`POST /api/v1/auth/logout` → `204`.

### Chamada OMV (⚠️ a validar)

`POST {OMV_URL}/rpc.php` com `{"service":"Session","method":"login","params":{"username":"...","password":"..."},"options":null}`.
Resposta esperada: `{"response":{"authenticated":true,"username":"admin","permissions":{"role":"admin",...}},"error":null}`.
Cookie de sessão devolvido em `Set-Cookie: PHPSESSID=...`.

## Critérios de aceite

- A01.1 Login com credenciais válidas de admin retorna 200 e define o cookie.
- A01.2 Login com credenciais inválidas retorna 401 e não define cookie.
- A01.3 Login de usuário não-admin retorna 403 e chama `Session.logout`.
- A01.4 Rota protegida sem cookie retorna 401; com cookie válido retorna 200.
- A01.5 Logout invalida o cookie: a mesma sessão volta a dar 401.
- A01.6 Mutação sem `X-Requested-With` retorna 403.
- A01.7 Sessão acima do TTL deslizante retorna 401.
- A01.8 UI: formulário de login com erro claro; rotas protegidas redirecionam ao login.
- A01.9 Erro RPC com mensagem de sessão expirada (código qualquer) e HTTP 401 de `/rpc.php` viram `ErrSessionExpired`;
  HTML com HTTP 200 continua sendo erro do OMV (não expiração).
- A01.10 O keep-alive chama o OMV só para sessões ativas, descarta as que o OMV declarou expiradas e ignora outras falhas.
