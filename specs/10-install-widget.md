# SPEC-10 — Painel de instalações

## Contexto

Hoje o progresso de uma instalação só aparece na tela do app (SPEC-05 R05.9). Se o usuário sai dela, perde a
visibilidade; e, se a instalação falha, o log some junto com a tela. Esta spec adiciona um **widget global**,
visível em todas as telas, que acompanha **várias instalações ao mesmo tempo** e dá acesso ao **log completo**,
inclusive depois de uma falha.

## Backend

As instalações já rodam em segundo plano e podem ser simultâneas (uma por app). Cada uma é um `Job` em memória
com todos os eventos (`starting`, `pull`, `log`, `done`, `error`).

- R10.1 (M) `GET /api/v1/store/installs` lista as instalações conhecidas, mais recentes primeiro:
  `{ id, title, icon, state, startedAt, finishedAt?, message }`. `state` é `running`, `done` ou `error`;
  `message` é a última linha de progresso (ou a mensagem de erro).
- R10.2 (M) `GET /api/v1/store/installs/{id}/events` (já existente) reproduz **todos** os eventos desde o início, também
  de instalações já terminadas, e encerra o stream ao final. É a fonte do log completo.
- R10.3 (M) Instalações terminadas (com sucesso ou erro) continuam listadas e com o log disponível até o usuário
  dispensá-las ou por 24 h, o que ocorrer primeiro. Uma nova instalação do mesmo app substitui a anterior.
- R10.4 (M) `DELETE /api/v1/store/installs/{id}` dispensa uma instalação terminada (`204`); em andamento → `409`;
  desconhecida → `404`.
- R10.5 (S) O log de uma instalação guarda no máximo 5000 linhas de saída; os eventos `done` e `error` nunca são
  descartados.
- R10.6 (S) Os jobs vivem na memória do serviço: reiniciar o OMV Home limpa a lista (apps já instalados não são afetados).

## Frontend

- R10.7 (M) O widget aparece em todas as telas autenticadas (canto inferior direito; acima da barra de navegação no
  celular) enquanto houver instalações a mostrar, e não aparece se não houver nenhuma.
- R10.8 (M) Recolhido, mostra um resumo: quantas estão em andamento e quantas falharam. Expandido, lista cada
  instalação com ícone, nome, estado (andamento / concluída / falhou) e a última linha de progresso.
- R10.9 (M) Clicar numa instalação abre o **log completo** (o mesmo log ao vivo para as em andamento). Falhas mostram a
  mensagem de erro em destaque e o log permanece disponível; há um botão para copiar o log.
- R10.10 (M) Instalações com falha ficam no widget até serem dispensadas (botão ×, que chama o `DELETE`). As concluídas
  somem do widget sozinhas após 60 s, e podem ser dispensadas antes; as concluídas têm o atalho "Abrir" para o app.
- R10.11 (M) Quando uma instalação termina em qualquer tela, o widget avisa com um toast (sucesso ou falha) e atualiza
  as listas de apps e da loja.
- R10.12 (M) Iniciar uma instalação (loja ou app personalizado) atualiza o widget imediatamente, sem esperar o próximo ciclo.
- R10.13 (S) O widget consulta `GET /store/installs` a cada 2 s enquanto há instalações em andamento e a cada 15 s
  caso contrário.

## Critérios de aceite

- A10.1 Com duas instalações simultâneas, `GET /store/installs` lista as duas com estados independentes.
- A10.2 Após uma falha, `GET /store/installs` mostra `error` com a mensagem e `/events` reproduz o log inteiro.
- A10.3 `DELETE` de instalação terminada remove-a da lista; em andamento → 409; inexistente → 404.
- A10.4 Jobs terminados há mais de 24 h somem da lista.
- A10.5 UI: o widget mostra duas instalações em andamento e uma falha; clicar na falha abre o log com a mensagem de erro.
- A10.6 UI: dispensar chama o `DELETE`; a transição andamento → concluída gera toast e invalida apps e loja.
