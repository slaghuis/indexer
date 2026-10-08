# Go Indexer for Qdrant
A production-grade indexer that uses Go's AST to chunk code semantically, embeds via Ollama, and upserts to Qdrant. Idempotent, incremental, and watch-mode capable.

## Design Decisions
 | Decision | Choice | Why | 
 | ------------ | ---------------- | ----------------| 
 | Chunking unit | Function/method/type declaration | Matches how devs think & query |
 | Chunk enrichment | Include package name, imports, godoc, receiver | Dramatically improves retrieval  | 
 | Point ID | Deterministic hash of repo+path+symbol | Idempotent upserts, easy deletes | 
 | Change detection | SHA256 of chunk body | Skip unchanged chunks → fast re-index | 
 | Embedding | Ollama nomic-embed-text (768-dim) | Fast on M4, free | 
 | Vector DB client | github.com/qdrant/go-client (gRPC) | Fastest option | 
 | Concurrency | Worker pool with bounded channel | Saturates Ollama without OOM | 
 | Watch mode | fsnotify + debounce | Reindex on save | 


## Run it
`
# Make sure Qdrant and Ollama are running
docker ps | grep qdrant
curl -s http://localhost:11434/api/tags | jq .

go run ./cmd/indexer -config config.yaml           # one-shot
go run ./cmd/indexer -config config.yaml -watch    # daemon mode
`

Verify in Qdrant
`
# Collection info
curl -s http://localhost:6333/collections/code_chunks | jq .

# Try a search (embed a query first via Ollama)
QUERY_VEC=$(curl -s http://localhost:11434/api/embed \
  -d '{"model":"nomic-embed-text","input":"http middleware for auth"}' \
  | jq -c '.embeddings[0]')

curl -s -X POST http://localhost:6333/collections/code_chunks/points/search \
  -H 'Content-Type: application/json' \
  -d "{\"vector\": $QUERY_VEC, \"limit\": 5, \"with_payload\": true}" \
  | jq '.result[] | {score, symbol: .payload.symbol, path: .payload.path}'
`

## Operational Notes
 - **First run** on a ~100k LOC repo: ~2–4 minutes on an M4 with concurrency: 4.
 - **Incremental runs**: typically <5 seconds since unchanged chunks are skipped via hash.
 - **Memory**: steady ~80MB for the indexer; Ollama is the bigger process.
 - **Multiple repos**: run the binary multiple times with different config.yaml files, same collection, different repo field. Payload filter on repo isolates searches.
 - **Git hook**: add .git/hooks/post-commit that runs the indexer in one-shot mode for a free "always fresh" setup without the watcher.
 - **Test files**: deliberately excluded. Flip the check if you want them.

