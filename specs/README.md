# OMV Home — Especificações

Metodologia: **spec-driven development**. Cada funcionalidade tem uma spec com requisitos
numerados, contrato de API e critérios de aceite. O código só é escrito depois da spec, e cada
feature implementada vira um commit que referencia o ID da spec (ex.: `feat(auth): ... [SPEC-01]`).

| ID | Spec | Status |
|----|------|--------|
| 00 | [Visão geral e arquitetura](00-overview.md) | Implementada |
| 01 | [Autenticação](01-auth.md) | Implementada |
| 02 | [Dashboard e sistema](02-dashboard.md) | Implementada |
| 03 | [Containers](03-containers.md) | Implementada |
| 04 | [Apps (home estilo ZimaOS)](04-apps.md) | Implementada |
| 05 | [App Store](05-app-store.md) | Implementada |
| 06 | [Armazenamento e compartilhamentos](06-storage.md) | Implementada |
| 07 | [Gerenciador de arquivos](07-file-manager.md) | **Somente spec** (implementação futura) |
| 08 | [Empacotamento e deploy](08-packaging.md) | Implementada (A08.2 `docker build` ainda não verificado: sem Docker na máquina de desenvolvimento) |
| 09 | [Fontes de apps personalizadas (ZimaOS/Umbrel)](09-custom-sources.md) | Implementada (conversão validada com os repositórios reais; instalação de apps de terceiros não testada em Docker real) |
| 10 | [Painel de instalações (widget global)](10-install-widget.md) | Em implementação |

## Convenções

- Requisitos usam o formato EARS: *"Quando <evento>, o sistema deve <resposta>"*.
- IDs de requisito: `R<spec>.<n>` (ex.: `R03.4`). Critérios de aceite: `A<spec>.<n>`.
- Prioridade: **M** (must, MVP), **S** (should), **C** (could).
- Mudou o comportamento? Atualize a spec no mesmo commit do código.
- Todo trecho que depende de comportamento do OMV real e que não foi validado contra uma
  instância OMV 7 está marcado com ⚠️ **a validar** e fica isolado em `backend/internal/omv`.
