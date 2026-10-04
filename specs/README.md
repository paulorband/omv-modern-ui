# OMV Home — Especificações

Metodologia: **spec-driven development**. Cada funcionalidade tem uma spec com requisitos
numerados, contrato de API e critérios de aceite. O código só é escrito depois da spec, e cada
feature implementada vira um commit que referencia o ID da spec (ex.: `feat(auth): ... [SPEC-01]`).

| ID | Spec | Status |
|----|------|--------|
| 00 | [Visão geral e arquitetura](00-overview.md) | Aprovada |
| 01 | [Autenticação](01-auth.md) | MVP |
| 02 | [Dashboard e sistema](02-dashboard.md) | MVP |
| 03 | [Containers](03-containers.md) | MVP |
| 04 | [Apps (home estilo ZimaOS)](04-apps.md) | MVP |
| 05 | [App Store](05-app-store.md) | MVP |
| 06 | [Armazenamento e compartilhamentos](06-storage.md) | MVP |
| 07 | [Gerenciador de arquivos](07-file-manager.md) | **Somente spec** (implementação futura) |
| 08 | [Empacotamento e deploy](08-packaging.md) | MVP |

## Convenções

- Requisitos usam o formato EARS: *"Quando <evento>, o sistema deve <resposta>"*.
- IDs de requisito: `R<spec>.<n>` (ex.: `R03.4`). Critérios de aceite: `A<spec>.<n>`.
- Prioridade: **M** (must, MVP), **S** (should), **C** (could).
- Mudou o comportamento? Atualize a spec no mesmo commit do código.
- Todo trecho que depende de comportamento do OMV real e que não foi validado contra uma
  instância OMV 7 está marcado com ⚠️ **a validar** e fica isolado em `backend/internal/omv`.
