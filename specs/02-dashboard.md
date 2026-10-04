# SPEC-02 — Dashboard e sistema

## Contexto

Visão rápida da saúde do servidor, exibida na home ao lado dos apps (widgets).

## Requisitos

- R02.1 (M) `GET /api/v1/system` retorna hostname, versão do OMV (⚠️), kernel, uptime e
  número de núcleos.
- R02.2 (M) `GET /api/v1/system/stats` retorna um instantâneo: uso de CPU (%), memória (total,
  usada, disponível), swap, carga média (1/5/15), temperatura quando disponível, e tráfego de
  rede acumulado por interface (rx/tx bytes).
- R02.3 (M) `GET /api/v1/system/stats/stream` (SSE) emite o mesmo objeto a cada 2 s enquanto a
  conexão estiver aberta; para de calcular quando não há clientes.
- R02.4 (M) Uso de CPU é calculado por diferença entre duas leituras de `/proc/stat`.
- R02.5 (M) `GET /api/v1/system/disks` lista filesystems montados (via OMV
  `FileSystemMgmt.enumerateFilesystems` ⚠️): dispositivo, rótulo, tipo, ponto de montagem,
  total, usado, livre, percentual.
- R02.6 (S) Incluir status SMART resumido por disco quando o OMV expuser (⚠️
  `Smart.getList`). Ausência não deve causar erro.
- R02.7 (M) Frontend: widgets de CPU, memória, rede e armazenamento com atualização por SSE,
  barras acessíveis (texto + cor) e estado de erro/carregando.
- R02.8 (M) Em plataformas sem `/proc` (modo mock em Windows/macOS), o coletor deve devolver
  valores simulados determinísticos, sem falhar.

## Contrato

```json
// GET /api/v1/system/stats
{
  "time": "2026-10-04T12:00:00Z",
  "cpu": { "percent": 12.5, "cores": 4, "load": [0.2, 0.3, 0.3] },
  "memory": { "total": 8589934592, "used": 2147483648, "available": 6442450944 },
  "swap": { "total": 0, "used": 0 },
  "temperature": 41.0,
  "network": [{ "name": "eth0", "rxBytes": 123, "txBytes": 456 }]
}
```

## Critérios de aceite

- A02.1 Dado um `/proc/stat` de fixture em dois instantes, o percentual de CPU calculado confere.
- A02.2 Dado um `/proc/meminfo` de fixture, memória usada = total − disponível.
- A02.3 O stream SSE entrega ≥ 2 eventos em ~5 s no teste.
- A02.4 Disco com erro no OMV resulta em `502 upstream_error`, não em `500`.
- A02.5 UI mostra os quatro widgets com dados do mock.
