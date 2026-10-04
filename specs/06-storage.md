# SPEC-06 — Armazenamento e compartilhamentos

## Contexto

Camada simplificada sobre os recursos de storage do OMV. Operações avançadas (RAID, criar/formatar
filesystem, quotas) continuam na UI original; a nova UI oferece um link "Configurações avançadas"
para ela.

Toda a integração fica em `internal/omv` (⚠️ a validar contra OMV 7).

## Requisitos

- R06.1 (M) `GET /api/v1/storage/filesystems`: filesystems montados e utilizáveis (reaproveita
  SPEC-02 R02.5, com `uuid` para uso na criação de shares).
- R06.2 (M) `GET /api/v1/storage/shares`: pastas compartilhadas do OMV (`ShareMgmt.getList`),
  com nome, caminho absoluto resolvido, filesystem, permissões, comentário e serviços ativos
  (SMB, NFS) quando disponíveis.
- R06.3 (M) `POST /api/v1/storage/shares` cria uma pasta compartilhada: `{ name, filesystemUuid,
  relativePath, comment, smb: bool, smbGuest: bool }`. Fluxo: `ShareMgmt.set` → (se `smb`)
  `SMB.setShare` → `Config.applyChanges`. Validar nome (`^[a-zA-Z0-9_-]{1,32}$`) e caminho relativo
  (sem `..`, sem barra inicial).
- R06.4 (M) Se qualquer etapa falhar após a criação, o sistema deve reverter o que fez
  (`ShareMgmt.delete`) quando possível e retornar `502 upstream_error` com a etapa que falhou.
- R06.5 (M) `DELETE /api/v1/storage/shares/{uuid}` remove o share (nunca apaga os dados em disco)
  e aplica as mudanças.
- R06.6 (S) `PATCH /api/v1/storage/shares/{uuid}` liga/desliga SMB e acesso convidado.
- R06.7 (M) `GET /api/v1/storage/users`: usuários do OMV (somente leitura no MVP).
- R06.8 (M) Toda mutação termina com `Config.applyChanges` e aguarda a conclusão do job
  (⚠️ o OMV pode retornar um nome de arquivo de progresso em background); timeout de 60 s.
- R06.9 (M) Frontend, **Armazenamento**: cards de disco (barra de uso + estado), lista de shares
  com status SMB, assistente "Novo compartilhamento" (3 passos: onde → nome → acesso), e link
  para a UI original do OMV (`http://<host>/`) em "Configurações avançadas".
- R06.10 (M) Aviso na UI quando o share estiver dentro de um filesystem quase cheio (> 90 %).

## Contrato (resumo)

```json
// GET /api/v1/storage/shares
[{ "uuid": "…", "name": "media", "path": "/srv/dev-disk-by-uuid-…/media",
   "filesystemUuid": "…", "comment": "", "smb": true, "nfs": false }]
```

## Critérios de aceite

- A06.1 Com um OMV falso (httptest) a criação de share encadeia `ShareMgmt.set` → `SMB.setShare`
  → `Config.applyChanges` na ordem certa, com os parâmetros esperados.
- A06.2 Falha em `SMB.setShare` dispara `ShareMgmt.delete` e retorna 502.
- A06.3 Nome inválido → 400 sem chamar o OMV.
- A06.4 Remover share não toca no filesystem (o código não tem chamada de remoção de diretórios).
- A06.5 UI: assistente bloqueia avanço com nome inválido e mostra erro do backend.
