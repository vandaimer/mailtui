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

Teclas: `↑/↓` ou `j/k` navegam, `Enter` abre, `Esc` ou `←` volta, `PgUp/PgDn`
rolam mensagens longas e `q` sai.

Nesta primeira versão são exibidos as pastas, mensagens, headers principais,
corpo `text/plain` (com fallback simples de HTML para texto) e metadados dos
anexos MIME. Mensagens ilegíveis aparecem como inválidas, ajudando a verificar
a integridade do backup sem interromper a navegação.
