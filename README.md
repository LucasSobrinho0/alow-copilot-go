# Alow Telecom Invoice Extractor

## Diagnóstico local de PDFs

O comando abaixo lê todos os arquivos `.pdf` diretamente em `media/` e cria
um `.txt` correspondente em `media/pdftotext/`:

```bash
sudo docker compose run --rm app pdftotext
```

O recurso é bloqueado por padrão. Defina `PDF_TO_TEXT=true` no `.env` para
habilitá-lo. Com `false`, o comando encerra sem criar diretório ou arquivo.
O Compose executa com `LOCAL_UID` e `LOCAL_GID`, que devem corresponder ao
proprietário da pasta `media/`; os valores padrão já refletem o ambiente Alow.

Cada `.txt` contém exatamente o texto bruto retornado por
`pdftotext -layout`, incluindo quebras de linha e espaçamento. O diagnóstico
não executa os parsers de Vivo ou Claro. Os arquivos são publicados de forma
atômica para não deixar uma saída parcial parecendo válida.

## Proteção de recursos

O extrator processa no máximo `MAX_CONCURRENT_EXTRACTIONS` documentos ao mesmo
tempo (padrão `2`). Quando todas as posições estão ocupadas, a API retorna `429`
com `Retry-After`, permitindo que a fila do Laravel tente novamente sem criar
pressão adicional. `GOMEMLIMIT` e o limite de memória do container ficam em
`512MiB` e `768m` por padrão.

O `pdftotext` escreve em arquivo temporário. O parser Claro lê esse arquivo
linha a linha e libera cada bloco de telefone em lotes de `ITEM_CHUNK_SIZE`;
portanto, a memória não cresce proporcionalmente ao número total de linhas da
fatura.
