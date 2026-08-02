# mailtui

Leitor TUI, offline e **somente leitura**, para backups de e-mail em formato
Maildir. A raiz pode conter várias pastas/labels; cada diretório que possua
`cur/`, `new/` e `tmp/` é descoberto automaticamente.

## Uso

```sh
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o mailtui .
./mailtui /mnt/mail
```

O resultado é um único executável estático, sem runtime ou bibliotecas do
projeto para instalar. Não há acesso ao Gmail, OAuth, IMAP ou SMTP. O programa
apenas percorre diretórios e abre as mensagens de `cur/` e `new/` para leitura;
`tmp/` só é usado para reconhecer a estrutura.

Em terminais largos, a interface mostra simultaneamente pastas, mensagens e a
prévia do e-mail selecionado. Em larguras médias a lista e a leitura ficam
empilhadas; em terminais estreitos, cada painel ocupa a tela para continuar
legível.

Teclas:

- `↑/↓` ou `j/k`: navegar no painel em foco;
- `Tab`, `Shift+Tab`, `←/→` ou `h/l`: mudar o foco;
- `/`: buscar por assunto, remetente ou destinatários;
- `Enter`: confirmar a busca ou avançar para o próximo painel;
- `Esc`: cancelar a busca, limpar o filtro ou voltar;
- `PgUp/PgDn`: rolar o corpo da mensagem;
- `o`: escolher e abrir um anexo com o aplicativo padrão;
- `q`: sair.

`INBOX` aparece primeiro, seguida pelas pastas Gmail/sistema e pelas labels do
usuário em ordem natural. São exibidos headers principais, corpo `text/plain`
(com fallback simples de HTML para texto) e metadados dos anexos MIME.
Mensagens ilegíveis aparecem como inválidas, ajudando a verificar a integridade
do backup sem interromper a navegação.

## Backups em rede

A navegação nunca espera por I/O no loop da interface. Ao selecionar uma pasta,
o programa lê apenas os headers das mensagens, com concorrência limitada, e
mantém o resultado em memória. O corpo MIME completo — incluindo anexos — só é
lido para a mensagem selecionada. Pequenas pausas na seleção usam debounce para
que atravessar rapidamente a lista de pastas ou mensagens não dispare leituras
desnecessárias no ponto de montagem remoto.

Na primeira leitura de uma pasta, os headers aparecem progressivamente em
lotes. Ao terminar, o programa guarda somente esses metadados em
`~/.cache/mailtui/metadata-v1/`. Nas execuções seguintes ele compara a lista de
arquivos do Maildir e reutiliza o cache se ela não mudou. Nenhum índice ou cache
é criado dentro do backup.

## Anexos

Com uma mensagem carregada, pressione `o`, escolha o anexo e confirme com
`Enter`. O conteúdo é decodificado para `~/.cache/mailtui/attachments/` com
permissões restritas e aberto via `xdg-open` — por exemplo, um PDF vai para o
visualizador padrão do desktop. Os arquivos originais permanecem intactos.
