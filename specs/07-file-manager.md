# SPEC-07 — Gerenciador de arquivos

> **Status: somente especificação.** A implementação fica para depois do MVP. Esta spec existe
> para que as decisões de segurança e de API já estejam tomadas e o MVP não crie conflitos
> (por exemplo, a rota `/files` e o prefixo `/api/v1/files` ficam reservados).

## Contexto

Navegar, enviar, baixar e organizar arquivos dentro dos shares do OMV pelo navegador, no estilo
do app Arquivos do ZimaOS.

## Escopo

Raízes navegáveis = apenas pastas compartilhadas do OMV (SPEC-06) e `APPDATA_DIR`.
Nada fora disso é acessível, nem por link simbólico.

## Requisitos

### Navegação

- R07.1 (M) `GET /api/v1/files?path=/media/filmes` lista o diretório: nome, tipo (arquivo,
  pasta, link), tamanho, modificado em, permissões; ordenação e paginação (`limit`, `cursor`).
- R07.2 (M) O caminho virtual começa pelo nome do share (`/media/...`). O backend resolve para o
  caminho real e **rejeita** qualquer resultado fora da raiz após `filepath.EvalSymlinks`
  (anti path traversal e symlink escape).
- R07.3 (M) `GET /api/v1/files/roots` lista as raízes disponíveis.
- R07.4 (S) Busca por nome dentro de uma pasta (`GET /api/v1/files/search?path=&q=`), limitada
  em profundidade e tempo.

### Operações

- R07.5 (M) Criar pasta, renomear, mover, copiar e excluir (para lixeira por padrão).
- R07.6 (M) Lixeira por share em `.omvhome-trash/` com restauração e esvaziamento; exclusão
  definitiva exige confirmação na UI.
- R07.7 (M) Operações longas (copiar/mover pastas grandes) rodam como **jobs**:
  `POST /api/v1/files/jobs` → `202 { jobId }`; progresso via SSE; cancelamento via `DELETE`.
- R07.8 (M) Conflito de nome retorna `409` com opções `replace | keep-both | skip` enviadas no
  corpo da requisição.

### Transferência

- R07.9 (M) Download de arquivo com suporte a `Range` e `Content-Disposition` correto.
- R07.10 (M) Download de pasta como ZIP em streaming (sem materializar em disco).
- R07.11 (M) Upload retomável em partes (protocolo tus ou equivalente simples:
  `POST` cria, `PATCH` com `Upload-Offset`); limite de tamanho configurável.
- R07.12 (S) Prévia: imagens (miniaturas em cache), vídeo/áudio (stream com `Range`), texto, PDF.

### Permissões e auditoria

- R07.13 (M) As operações rodam como o usuário do SO correspondente ao usuário do OMV
  (setuid via helper com privilégios mínimos), nunca como root pleno. Alternativa no MVP do
  gerenciador: executar como o usuário do serviço e respeitar as ACLs dos shares do OMV.
- R07.14 (M) Cada mutação gera log de auditoria estruturado (usuário, operação, caminho).
- R07.15 (S) Compartilhar por link com expiração e senha opcional (fase posterior).

### Frontend

- R07.16 (M) Visualização em lista e grade, breadcrumb, seleção múltipla, arrastar-e-soltar para
  upload e para mover, menu de contexto, atalhos de teclado (Del, F2, Ctrl+A).
- R07.17 (M) Painel de transferências com progresso e cancelamento.

## Modelo de dados

```ts
type FileEntry = {
  name: string; path: string; kind: 'file' | 'dir' | 'symlink';
  size: number; modifiedAt: string; mode: string; mime?: string;
};
```

## Riscos e decisões em aberto

| Tema | Decisão proposta |
|------|------------------|
| Execução com o usuário correto | Helper dedicado com `CAP_SETUID/SETGID`, ou rodar o serviço como root restrito por AppArmor/systemd (`ProtectSystem`, `ReadWritePaths`). Decidir na implementação. |
| Symlinks | Seguir somente se o destino resolvido ficar dentro da raiz. |
| Arquivos grandes | Sempre streaming, nunca carregar em memória. |
| Miniaturas | Cache em `DATA_DIR/thumbs`, geradas sob demanda com limite de concorrência. |

## Critérios de aceite (para a futura implementação)

- A07.1 `../` e links simbólicos que escapam da raiz retornam 403.
- A07.2 Download com `Range: bytes=10-19` retorna 206 com o trecho correto.
- A07.3 ZIP de pasta de 5 GB usa memória constante (teste de uso de memória).
- A07.4 Upload interrompido é retomado do offset correto.
- A07.5 Excluir envia para a lixeira; restaurar devolve ao local original.
