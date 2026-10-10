#!/usr/bin/env bash
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/../.." && pwd)
case "${1:---list}" in
  --list)
    printf '%s\n' 'P01: full Go regression plus enabled/disabled Prometheus tests' \
      'P01 live: scrape a running API health endpoint and separate Prometheus endpoint' \
      'P02 [--degraded|--complete] [--existing]: config/dependency preflight + redacted report' \
      'P02-deps {up|status|logs|down}: manage only the named local acceptance dependency stack' \
      'P02-cleanup REPORT_DIR --confirm: remove only the P02 fixture from one report' \
      'batch {1|2|3|4} [--local]: run one delivery batch and write an aggregate report' \
      'P03: non-mutating API generation/compile check (no live services)' \
      'P04 --local: deterministic lifecycle + HTTP contract regression' \
      'P04: live isolated API/worker lifecycle, report + resource ledger' \
      'P04 --expect-paused-worker: verify queued state, then manually resume worker' \
      'P04 --expect-queue-outage: verify publish error, then manually restore Redis/worker' \
      'P05 [--local]: product split, stable IDs, unchanged replay, and incremental update' \
      'P06 [--local]: retrieval channels, granularity, budgets, and degradation' \
      'P07 [--local]: SSE routes, frame format, and chat validation' \
      'P08 [--local]: approval decision, checkpoint resume, and single-use execution' \
      'P09 [--local]: subject-to-dataset binding refresh and authorization' \
      'P10 [--local]: idempotent document enable/disable, parent path guard, and search visibility' \
      'P11 [--local]: no-ES structured exact matching, mapping normalization, filtering, and fusion' \
      'P12 [--local]: multipart upload contract, size/file-count limits, cleanup, and partial-failure ledger' \
      'P13 [--local]: bounded recursive directory import, dry-run, stable source, incremental replay, and retry' \
      'P14 [--local]: provider-neutral HTTP rerank adapter and observable reorder' \
      'P15 [--local]: rerank candidate budget, timeout, invalid-result fallback, and degradation report' \
      'P16 [--local]: LLM product split contract, schema validation, and failure boundary' \
      'P17 [--local]: LLM split replay fingerprint, stable keys, and incremental idempotency' \
      'P18 [--local]: workspace specialist skill-tool wiring and read-only boundary' \
      'P19 [--local]: provider-neutral user sync, dry-run, idempotency, and no-delete boundary' \
      'P24 [--local]: maintenance dry-run/report and mutation-count contract' \
      'P25 [--local]: business metrics, queue depth, retrieval degradation, and low-cardinality labels' \
      'P26 [--local]: HTTP-to-worker trace propagation and collector config contract' \
      'P27 [--local]: explicit additive migration plan, dry-run, and safe startup boundary' \
      'P28 [--local]: shadow index version build/progress/ready contract' \
      'P29 [--local]: index switch/rollback safety and idempotent failure contract' \
      'P20 [--local]: offline evaluation dataset, threshold, runner, and report contract' \
      'P21 [--local]: non-empty private corpus and cross-subject leakage gate' \
      'P22 [--local]: no-answer, required-fact, and citation correctness contract' \
      'P23 [--local]: reproducible local quality gate and machine-readable summary'
    ;;
  P01)
    [ "$#" -eq 1 ] || { echo 'P01 accepts no options; use verify-metrics.sh for a running service' >&2; exit 2; }
    REPORT="$ROOT/logs/acceptance/P01/$(date +%Y%m%d-%H%M%S)-$$"
    mkdir -p "$REPORT"
    if (cd "$ROOT" && go test -count=1 ./...) > "$REPORT/full-go-test.log" 2>&1; then
      cat "$REPORT/full-go-test.log"
      echo 'PASS P01: full Go regression and Prometheus enabled/disabled tests passed'
      echo "report: $REPORT"
    else
      status=$?; cat "$REPORT/full-go-test.log"; echo "P01 failed; report: $REPORT" >&2; exit "$status"
    fi
    ;;
  P01-live)
    [ "$#" -le 3 ] || { echo 'usage: P01-live [health-base-url] [metrics-url]' >&2; exit 2; }
    exec "$ROOT/scripts/acceptance/verify-metrics.sh" "${2:-http://127.0.0.1:8090}" "${3:-http://127.0.0.1:9091/metrics}"
    ;;
  P02)
    MODE=degraded
    CONFIG_SET=acceptance
    for arg in "${@:2}"; do
      case "$arg" in
        --degraded) MODE=degraded ;;
        --complete) MODE=complete ;;
        --existing) CONFIG_SET=existing ;;
        *) echo 'usage: P02 [--degraded|--complete] [--existing]' >&2; exit 2 ;;
      esac
    done
    if [ "$CONFIG_SET" = existing ]; then
      DEFAULT_API_CONFIG="$ROOT/configs/api/config.yaml"
      DEFAULT_WORKER_CONFIG="$ROOT/configs/worker/config.yaml"
      DEFAULT_REST_CONFIG="$ROOT/configs/api/restapi.yaml"
    else
      DEFAULT_API_CONFIG="$ROOT/configs/acceptance/api.yaml"
      DEFAULT_WORKER_CONFIG="$ROOT/configs/acceptance/worker.yaml"
      DEFAULT_REST_CONFIG="$ROOT/configs/acceptance/restapi.yaml"
    fi
    REPORT="$ROOT/logs/acceptance/P02/$(date +%Y%m%d-%H%M%S)-$$-$MODE-$CONFIG_SET"
    mkdir -p "$REPORT"
    ARGS=(
      go run ./cmd/acceptance-preflight
      -mode "$MODE"
      -api-config "${EINO_API_CONFIG:-$DEFAULT_API_CONFIG}"
      -worker-config "${EINO_WORKER_CONFIG:-$DEFAULT_WORKER_CONFIG}"
      -rest-config "${EINO_REST_CONFIG:-$DEFAULT_REST_CONFIG}"
      -report "$REPORT/preflight.json"
    )
    if [ "${EINO_P02_PROBE_EMBEDDING:-0}" = '1' ]; then
      ARGS+=( -probe-embedding )
    fi
    if (cd "$ROOT" && "${ARGS[@]}") > "$REPORT/preflight.log" 2>&1; then
      cat "$REPORT/preflight.log"
      cp "$REPORT/preflight.json" "$REPORT/summary.json"
      echo "PASS P02 [$MODE]; report: $REPORT"
    else
      status=$?
      cat "$REPORT/preflight.log"
      [ -f "$REPORT/preflight.json" ] && cp "$REPORT/preflight.json" "$REPORT/summary.json"
      echo "P02 blocked/failed [$MODE]; report: $REPORT" >&2
      exit "$status"
    fi
    ;;
  P02-deps)
    [ "$#" -ge 2 ] || { echo 'usage: P02-deps {up|status|logs|down} [compose logs args...]' >&2; exit 2; }
    shift
    exec "$ROOT/scripts/acceptance/dependencies.sh" "$@"
    ;;
  batch)
    exec "$ROOT/scripts/acceptance/run-batch.sh" "${@:2}"
    ;;
  P02-cleanup)
    [ "$#" -eq 3 ] && [ "${3:-}" = '--confirm' ] || {
      echo 'usage: P02-cleanup REPORT_DIR --confirm' >&2
      exit 2
    }
    exec "$ROOT/scripts/acceptance/cleanup-p02.sh" "$2" --confirm
    ;;
  P03)
    [ "$#" -eq 1 ] || { echo 'P03 accepts no options' >&2; exit 2; }
    REPORT="$ROOT/logs/acceptance/P03/$(date +%Y%m%d-%H%M%S)-$$"
    mkdir -p "$REPORT"
    if bash "$ROOT/scripts/check-api.sh" > "$REPORT/check.log" 2>&1; then
      cat "$REPORT/check.log"; echo "PASS P03; report: $REPORT"
    else
      status=$?; cat "$REPORT/check.log"; echo "P03 failed/blocked; report: $REPORT" >&2; exit "$status"
    fi
    ;;
  P05)
    [ "$#" -le 2 ] || { echo 'usage: P05 [--local]' >&2; exit 2; }
    [ "${2:-}" = '' ] || [ "${2:-}" = '--local' ] || { echo 'usage: P05 [--local]' >&2; exit 2; }
    REPORT="$ROOT/logs/acceptance/P05/$(date +%Y%m%d-%H%M%S)-$$-local"
    mkdir -p "$REPORT"
    if (cd "$ROOT" && go test -count=1 ./internal/application/knowledge -run '^TestProductCatalogIncrementalUpsert$' -v) > "$REPORT/tests.log" 2>&1; then
      cat "$REPORT/tests.log"
      cat > "$REPORT/summary.json" <<EOF
{"plan":"P05","mode":"local","status":"passed","checks":["three product blocks","stable product IDs","unchanged replay","single-product incremental update","duplicate model rejection"]}
EOF
      echo "PASS P05 local product incremental contract; report: $REPORT"
    else
      status=$?; cat "$REPORT/tests.log"; echo "P05 local product incremental contract failed; report: $REPORT" >&2; exit "$status"
    fi
    ;;
  P06)
    [ "$#" -le 2 ] || { echo 'usage: P06 [--local]' >&2; exit 2; }
    [ "${2:-}" = '' ] || [ "${2:-}" = '--local' ] || { echo 'usage: P06 [--local]' >&2; exit 2; }
    REPORT="$ROOT/logs/acceptance/P06/$(date +%Y%m%d-%H%M%S)-$$-local"
    mkdir -p "$REPORT"
    if (cd "$ROOT" && go test -count=1 ./internal/rag ./internal/application/knowledge -run 'TestRetrieveKeepsKeywordResultsWhenEmbeddingFails|TestRetrieveDegradesToVectorWhenKeywordFails|TestRetrieveFailsOnlyWhenAllChannelsFail|TestExactChannelBringsBackStructuredMatch|TestGroupHitsByDocumentKeepsBestChunk|TestGranularityResolvesFromDatasetType|TestRecallWidensBudgetUntilDocumentsFill|TestFillFullContentLeavesChunkGranularityAlone' -v) > "$REPORT/tests.log" 2>&1; then
      cat "$REPORT/tests.log"
      cat > "$REPORT/summary.json" <<EOF
{"plan":"P06","mode":"local","status":"passed","checks":["exact/keyword/vector degradation","all-channel failure","structured exact match","document/chunk granularity","recall budget widening","content truncation metadata"]}
EOF
      echo "PASS P06 local retrieval contract; report: $REPORT"
    else
      status=$?; cat "$REPORT/tests.log"; echo "P06 local retrieval contract failed; report: $REPORT" >&2; exit "$status"
    fi
    ;;
  P07)
    [ "$#" -le 2 ] || { echo 'usage: P07 [--local]' >&2; exit 2; }
    [ "${2:-}" = '' ] || [ "${2:-}" = '--local' ] || { echo 'usage: P07 [--local]' >&2; exit 2; }
    REPORT="$ROOT/logs/acceptance/P07/$(date +%Y%m%d-%H%M%S)-$$-local"
    mkdir -p "$REPORT"
    if (cd "$ROOT" && go test -count=1 ./internal/transport/restapi -run 'TestStreamRoutesAreServed|TestChatRequestValidation|TestChatFramesUseTheGeneratedFormat' -v) > "$REPORT/tests.log" 2>&1; then
      cat "$REPORT/tests.log"
      cat > "$REPORT/summary.json" <<EOF
{"plan":"P07","mode":"local","status":"passed","checks":["SSE routes registered","chat request validation","generated SSE frame format"]}
EOF
      echo "PASS P07 local SSE contract; report: $REPORT"
    else
      status=$?; cat "$REPORT/tests.log"; echo "P07 local SSE contract failed; report: $REPORT" >&2; exit "$status"
    fi
    ;;
  P08)
    [ "$#" -le 2 ] || { echo 'usage: P08 [--local]' >&2; exit 2; }
    [ "${2:-}" = '' ] || [ "${2:-}" = '--local' ] || { echo 'usage: P08 [--local]' >&2; exit 2; }
    REPORT="$ROOT/logs/acceptance/P08/$(date +%Y%m%d-%H%M%S)-$$-local"
    mkdir -p "$REPORT"
    if (cd "$ROOT" && go test -count=1 ./internal/platform/persistence/approval ./internal/application/middleware -run 'TestApprovalLifecycleIsSingleUseAndAudited|TestNewStorePreservesDependencies|TestPolicyApprovalByRisk' -v) > "$REPORT/tests.log" 2>&1; then
      cat "$REPORT/tests.log"
      cat > "$REPORT/summary.json" <<EOF
{"plan":"P08","mode":"local","status":"passed","checks":["approval create/decision","checkpoint claim","single-use consume","audit records","risk approval policy"]}
EOF
      echo "PASS P08 local approval contract; report: $REPORT"
    else
      status=$?; cat "$REPORT/tests.log"; echo "P08 local approval contract failed; report: $REPORT" >&2; exit "$status"
    fi
    ;;
  P09)
    [ "$#" -le 2 ] || { echo 'usage: P09 [--local]' >&2; exit 2; }
    [ "${2:-}" = '' ] || [ "${2:-}" = '--local' ] || { echo 'usage: P09 [--local]' >&2; exit 2; }
    REPORT="$ROOT/logs/acceptance/P09/$(date +%Y%m%d-%H%M%S)-$$-local"
    mkdir -p "$REPORT"
    if (cd "$ROOT" && go test -count=1 ./internal/tool -run 'TestKnowledgeSearchUsesIdentityBoundDatasetsAndFormatsCitations|TestKnowledgeSearchRequiresIdentityForDynamicBindings|TestKnowledgeSearchReturnsEmptyMessageWithoutAuthorizedDatasets|TestKnowledgeSearchResolvesBindingsOnEveryInvocation' -v) > "$REPORT/tests.log" 2>&1; then
      cat "$REPORT/tests.log"
      cat > "$REPORT/summary.json" <<EOF
{"plan":"P09","mode":"local","status":"passed","checks":["identity-bound allowlist","missing identity rejection","empty authorized dataset behavior","binding refresh on every invocation"]}
EOF
      echo "PASS P09 local authorization contract; report: $REPORT"
    else
      status=$?; cat "$REPORT/tests.log"; echo "P09 local authorization contract failed; report: $REPORT" >&2; exit "$status"
    fi
    ;;
  P10)
    [ "$#" -le 2 ] || { echo 'usage: P10 [--local]' >&2; exit 2; }
    [ "${2:-}" = '' ] || [ "${2:-}" = '--local' ] || { echo 'usage: P10 [--local]' >&2; exit 2; }
    REPORT="$ROOT/logs/acceptance/P10/$(date +%Y%m%d-%H%M%S)-$$-local"
    mkdir -p "$REPORT"
    if (cd "$ROOT" && go test -count=1 ./internal/application/knowledge ./internal/transport/restapi/internal/handler/dataset -run 'TestDocumentEnableIsIdempotentAndScoped|TestDocumentHTTPContract' -v) > "$REPORT/tests.log" 2>&1; then
      cat "$REPORT/tests.log"
      cat > "$REPORT/summary.json" <<EOF
{"plan":"P10","mode":"local","status":"passed","checks":["explicit enabled target","idempotent retry","dataset/document parent guard","content retention","disabled search exclusion","re-enabled search visibility","HTTP missing-field and repeated-request contract"]}
EOF
      echo "PASS P10 local document enable contract; report: $REPORT"
    else
      status=$?; cat "$REPORT/tests.log"; echo "P10 local document enable contract failed; report: $REPORT" >&2; exit "$status"
    fi
    ;;
  P11)
    [ "$#" -le 2 ] || { echo 'usage: P11 [--local]' >&2; exit 2; }
    [ "${2:-}" = '' ] || [ "${2:-}" = '--local' ] || { echo 'usage: P11 [--local]' >&2; exit 2; }
    REPORT="$ROOT/logs/acceptance/P11/$(date +%Y%m%d-%H%M%S)-$$-local"
    mkdir -p "$REPORT"
    if (cd "$ROOT" && go test -count=1 ./internal/platform/storage/es ./internal/rag -run 'TestNoESStructuredExactSearchAndFusion|TestExactFieldsOnlyCoversDeclaredKeyword|TestNormalizeExact' -v) > "$REPORT/tests.log" 2>&1; then
      cat "$REPORT/tests.log"
      cat > "$REPORT/summary.json" <<EOF
{"plan":"P11","mode":"local","status":"passed","checks":["shared mapping fields","case/space normalization","full model exact match without ES","substring not mislabeled exact","disabled/dataset filtering","exact channel fusion/report","real PostgreSQL/HTTP/scale run pending"]}
EOF
      echo "PASS P11 local no-ES structured exact contract; report: $REPORT"
    else
      status=$?; cat "$REPORT/tests.log"; echo "P11 local no-ES structured exact contract failed; report: $REPORT" >&2; exit "$status"
    fi
    ;;
  P13)
    [ "$#" -le 2 ] || { echo 'usage: P13 [--local]' >&2; exit 2; }
    [ "${2:-}" = '' ] || [ "${2:-}" = '--local' ] || { echo 'usage: P13 [--local]' >&2; exit 2; }
    REPORT="$ROOT/logs/acceptance/P13/$(date +%Y%m%d-%H%M%S)-$$-local"
    mkdir -p "$REPORT"
    if (cd "$ROOT" && GOCACHE="${GOCACHE:-/tmp/eino-go-cache}" go test -count=1 ./internal/application/knowledge -run '^TestImportDirectory' -v) > "$REPORT/tests.log" 2>&1; then
      cat "$REPORT/tests.log"
      cat > "$REPORT/summary.json" <<EOF
{"plan":"P13","mode":"local","status":"passed","checks":["recursive markdown/text scan","stable logical source","dry-run has no writes","unchanged replay","single-file incremental update","failed-file retry","symlink escape rejection","missing source is not deleted"]}
EOF
      echo "PASS P13 local directory import contract; report: $REPORT"
    else
      status=$?; cat "$REPORT/tests.log"; echo "P13 local directory import contract failed; report: $REPORT" >&2; exit "$status"
    fi
    ;;
  P14)
    [ "$#" -le 2 ] || { echo 'usage: P14 [--local]' >&2; exit 2; }
    [ "${2:-}" = '' ] || [ "${2:-}" = '--local' ] || { echo 'usage: P14 [--local]' >&2; exit 2; }
    REPORT="$ROOT/logs/acceptance/P14/$(date +%Y%m%d-%H%M%S)-$$-local"
    mkdir -p "$REPORT"
    if (cd "$ROOT" && GOCACHE="${GOCACHE:-/tmp/eino-go-cache}" go test -count=1 ./internal/rag/rerank ./internal/rag -run 'TestHTTPReranker|TestRerankIsBoundedAndReportShowsApplied' -v) > "$REPORT/tests.log" 2>&1; then
      cat "$REPORT/tests.log"
      cat > "$REPORT/summary.json" <<EOF
{"plan":"P14","mode":"local","status":"passed","checks":["provider-neutral HTTP request contract","authorization is sent without logging key","original candidates are reordered by score","invalid result set is rejected","real external rerank provider/model remains environment-dependent"]}
EOF
      echo "PASS P14 local HTTP rerank contract; report: $REPORT"
    else
      status=$?; cat "$REPORT/tests.log"; echo "P14 local HTTP rerank contract failed; report: $REPORT" >&2; exit "$status"
    fi
    ;;
  P15)
    [ "$#" -le 2 ] || { echo 'usage: P15 [--local]' >&2; exit 2; }
    [ "${2:-}" = '' ] || [ "${2:-}" = '--local' ] || { echo 'usage: P15 [--local]' >&2; exit 2; }
    REPORT="$ROOT/logs/acceptance/P15/$(date +%Y%m%d-%H%M%S)-$$-local"
    mkdir -p "$REPORT"
    if (cd "$ROOT" && GOCACHE="${GOCACHE:-/tmp/eino-go-cache}" go test -count=1 ./internal/rag -run 'TestRerankIsBoundedAndReportShowsApplied|TestRerankFailureAndTimeoutKeepFusedResults|TestRerankInvalidSubsetIsRejected' -v) > "$REPORT/tests.log" 2>&1; then
      cat "$REPORT/tests.log"
      cat > "$REPORT/summary.json" <<EOF
{"plan":"P15","mode":"local","status":"passed","checks":["candidate count is bounded","timeout is enforced","failure preserves fused recall","invalid subset preserves fused recall","degraded report exposes rerank"]}
EOF
      echo "PASS P15 local rerank budget/degradation contract; report: $REPORT"
    else
      status=$?; cat "$REPORT/tests.log"; echo "P15 local rerank budget/degradation contract failed; report: $REPORT" >&2; exit "$status"
    fi
    ;;
  P16)
    [ "$#" -le 2 ] || { echo 'usage: P16 [--local]' >&2; exit 2; }
    [ "${2:-}" = '' ] || [ "${2:-}" = '--local' ] || { echo 'usage: P16 [--local]' >&2; exit 2; }
    REPORT="$ROOT/logs/acceptance/P16/$(date +%Y%m%d-%H%M%S)-$$-local"
    mkdir -p "$REPORT"
    if (cd "$ROOT" && go test -count=1 ./internal/rag/parser/llmsplit ./internal/application/knowledge -run 'Test(HTTPClientSplitValidatesAndSendsContract|NormalizeRejectsAmbiguousProducts|InputFingerprintIncludesParserVersionAndModel|BuildLLMProductDocuments)') > "$REPORT/tests.log" 2>&1; then
      cat "$REPORT/tests.log"
      cat > "$REPORT/summary.json" <<EOF
{"plan":"P16","mode":"local","status":"passed","checks":["provider-neutral HTTP request contract","bounded normalized product schema","duplicate/missing-key/empty-content rejection","no-front-matter conversion to product documents","model timeout/error remains a write boundary"],"real_model_validation":"pending model, cost, latency, and annotated samples"}
EOF
      echo "PASS P16 local LLM split contract; report: $REPORT"
    else
      status=$?; cat "$REPORT/tests.log"; echo "P16 local LLM split contract failed; report: $REPORT" >&2; exit "$status"
    fi
    ;;
  P17)
    [ "$#" -le 2 ] || { echo 'usage: P17 [--local]' >&2; exit 2; }
    [ "${2:-}" = '' ] || [ "${2:-}" = '--local' ] || { echo 'usage: P17 [--local]' >&2; exit 2; }
    REPORT="$ROOT/logs/acceptance/P17/$(date +%Y%m%d-%H%M%S)-$$-local"
    mkdir -p "$REPORT"
    if (cd "$ROOT" && go test -count=1 ./internal/rag/parser/llmsplit ./internal/application/knowledge -run 'Test(InputFingerprintIncludesParserVersionAndModel|BuildLLMProductDocuments|ProductCatalogIncrementalUpsert)') > "$REPORT/tests.log" 2>&1; then
      cat "$REPORT/tests.log"
      cat > "$REPORT/summary.json" <<EOF
{"plan":"P17","mode":"local","status":"passed","checks":["input/parser/model replay fingerprint","stable product key and source stem","parser version is persisted in metadata","existing unique-key upsert/conflict recovery","incremental unchanged/update behavior"],"real_concurrency_validation":"pending live API/DB/queue restart run"}
EOF
      echo "PASS P17 local LLM replay/idempotency contract; report: $REPORT"
    else
      status=$?; cat "$REPORT/tests.log"; echo "P17 local LLM replay/idempotency contract failed; report: $REPORT" >&2; exit "$status"
    fi
    ;;
  P18)
    [ "$#" -le 2 ] || { echo 'usage: P18 [--local]' >&2; exit 2; }
    [ "${2:-}" = '' ] || [ "${2:-}" = '--local' ] || { echo 'usage: P18 [--local]' >&2; exit 2; }
    REPORT="$ROOT/logs/acceptance/P18/$(date +%Y%m%d-%H%M%S)-$$-local"
    mkdir -p "$REPORT"
    if (cd "$ROOT" && GOCACHE="${GOCACHE:-/tmp/eino-go-cache}" go test -count=1 ./internal/application/agent ./internal/skill -v) > "$REPORT/tests.log" 2>&1 && \
       grep -q 'list_skills.*load_skill' "$ROOT/internal/application/agent/factory.go" && \
       grep -q 'list_skills first.*load_skill' "$ROOT/internal/application/agent/workspace.go"; then
      cat "$REPORT/tests.log"
      cat > "$REPORT/summary.json" <<EOF
{"plan":"P18","mode":"local","status":"passed","checks":["workspace specialist receives list_skills and load_skill","instruction requires list-before-load","Skill loader path and size boundary tests compile","write/execute powers remain outside Skill grant","real model tool-call trace remains environment-dependent"]}
EOF
      echo "PASS P18 local Skill wiring contract; report: $REPORT"
    else
      status=$?; cat "$REPORT/tests.log" 2>/dev/null || true; echo "P18 local Skill wiring contract failed; report: $REPORT" >&2; exit "$status"
    fi
    ;;
  P19)
    [ "$#" -le 2 ] || { echo 'usage: P19 [--local]' >&2; exit 2; }
    [ "${2:-}" = '' ] || [ "${2:-}" = '--local' ] || { echo 'usage: P19 [--local]' >&2; exit 2; }
    REPORT="$ROOT/logs/acceptance/P19/$(date +%Y%m%d-%H%M%S)-$$-local"
    mkdir -p "$REPORT"
    if (cd "$ROOT" && GOCACHE="${GOCACHE:-/tmp/eino-go-cache}" go test -count=1 ./internal/platform/usersource ./internal/application/usersync ./internal/transport/restapi/internal/logic/user -run 'Test(HTTPSource|Sync|SyncUsersReturnsUnavailable)' -v) > "$REPORT/tests.log" 2>&1; then
      cat "$REPORT/tests.log"
      cat > "$REPORT/summary.json" <<EOF
{"plan":"P19","mode":"local","status":"passed","checks":["provider-neutral HTTP source contract","created/updated/skipped/failed report","dry-run is read-only","stable external_id idempotency","upstream omissions never delete local accounts","disabled source returns 503"],"real_upstream_validation":"pending source endpoint, stable ID mapping, credentials, and sandbox acceptance"}
EOF
      echo "PASS P19 local user-sync contract; report: $REPORT"
    else
      status=$?; cat "$REPORT/tests.log"; echo "P19 local user-sync contract failed; report: $REPORT" >&2; exit "$status"
    fi
    ;;
  P24)
    [ "$#" -le 2 ] || { echo 'usage: P24 [--local]' >&2; exit 2; }
    [ "${2:-}" = '' ] || [ "${2:-}" = '--local' ] || { echo 'usage: P24 [--local]' >&2; exit 2; }
    REPORT="$ROOT/logs/acceptance/P24/$(date +%Y%m%d-%H%M%S)-$$-local"
    mkdir -p "$REPORT"
    if (cd "$ROOT" && GOCACHE="${GOCACHE:-/tmp/eino-go-cache}" go test -count=1 ./internal/maintenance ./cmd/maintenance -v) > "$REPORT/tests.log" 2>&1; then
      cat "$REPORT/tests.log"
      cat > "$REPORT/summary.json" <<EOF
{"plan":"P24","mode":"local","status":"passed","checks":["dry-run is read-only","eligible counts are reported by category","real run returns mutation counts","periodic worker runs immediately and stops on cancellation","worker config owns maintenance interval/retention/batch settings","CLI compiles with worker config","real database retention window remains environment-dependent"]}
EOF
      echo "PASS P24 local maintenance contract; report: $REPORT"
    else
      status=$?; cat "$REPORT/tests.log"; echo "P24 local maintenance contract failed; report: $REPORT" >&2; exit "$status"
    fi
    ;;
  P25)
    [ "$#" -le 2 ] || { echo 'usage: P25 [--local]' >&2; exit 2; }
    [ "${2:-}" = '' ] || [ "${2:-}" = '--local' ] || { echo 'usage: P25 [--local]' >&2; exit 2; }
    REPORT="$ROOT/logs/acceptance/P25/$(date +%Y%m%d-%H%M%S)-$$-local"
    mkdir -p "$REPORT"
    if (cd "$ROOT" && GOCACHE="${GOCACHE:-/tmp/eino-go-cache}" go test -count=1 ./internal/platform/observability ./internal/application/knowledge ./cmd/worker -run 'TestBusinessMetricsUseBoundedLabelsAndRecordValues|TestBusinessMetricsCanBeRegisteredForPrometheusAgent|TestHealthMetricKeepsOneActiveStatePerComponent|TestTraceEnvelopeKeepsHTTPToWorkerTraceID|^$' -v) > "$REPORT/tests.log" 2>&1 &&        grep -q 'queue_depth' "$ROOT/internal/platform/observability/metrics.go" &&        grep -q 'RecordRetrieval' "$ROOT/internal/application/knowledge/search.go"; then
      cat "$REPORT/tests.log"
      cat > "$REPORT/summary.json" <<EOF
{"plan":"P25","mode":"local","status":"passed","checks":["business metrics are registered in the Prometheus agent registry","queue depth is sampled by worker","index success/failure/retry metrics have bounded labels","retrieval hit/empty/error and degraded metrics are recorded","component health state is distinct from liveness","no document or user id labels"]}
EOF
      echo "PASS P25 local business observability contract; report: $REPORT"
    else
      status=$?; cat "$REPORT/tests.log"; echo "P25 local business observability contract failed; report: $REPORT" >&2; exit "$status"
    fi
    ;;
  P26)
    [ "$#" -le 2 ] || { echo 'usage: P26 [--local]' >&2; exit 2; }
    [ "${2:-}" = '' ] || [ "${2:-}" = '--local' ] || { echo 'usage: P26 [--local]' >&2; exit 2; }
    REPORT="$ROOT/logs/acceptance/P26/$(date +%Y%m%d-%H%M%S)-$$-local"
    mkdir -p "$REPORT"
    if (cd "$ROOT" && GOCACHE="${GOCACHE:-/tmp/eino-go-cache}" go test -count=1 ./internal/platform/queue/asynq ./internal/platform/observability -run 'TestTraceEnvelopeKeepsHTTPToWorkerTraceID' -v) > "$REPORT/tests.log" 2>&1 &&        grep -q 'otlp:' "$ROOT/configs/observability/otel-collector.yaml" &&        ! grep -Eiq 'password|api[_-]?key|authorization|document_body|request_body' "$ROOT/configs/observability/otel-collector.yaml"; then
      cat "$REPORT/tests.log"
      cat > "$REPORT/summary.json" <<EOF
{"plan":"P26","mode":"local","status":"passed","checks":["trace envelope preserves one trace id across publish and process spans","collector accepts OTLP gRPC/HTTP","collector pipeline has memory and batch bounds","collector config contains no credentials or document body fields","external collector UI and retry drill remain environment-dependent"]}
EOF
      echo "PASS P26 local trace propagation/collector contract; report: $REPORT"
    else
      status=$?; cat "$REPORT/tests.log"; echo "P26 local trace propagation/collector contract failed; report: $REPORT" >&2; exit "$status"
    fi
    ;;

  P27)
    [ "$#" -le 2 ] || { echo 'usage: P27 [--local]' >&2; exit 2; }
    [ "${2:-}" = '' ] || [ "${2:-}" = '--local' ] || { echo 'usage: P27 [--local]' >&2; exit 2; }
    REPORT="$ROOT/logs/acceptance/P27/$(date +%Y%m%d-%H%M%S)-$$-local"
    mkdir -p "$REPORT"
    if (cd "$ROOT" && GOCACHE="${GOCACHE:-/tmp/eino-go-cache}" go test -count=1 ./internal/platform/storage/entx -run 'TestMigrationPlanIsEmbeddedAndNonDestructive|TestPlannedMigrationsDoesNotExposeSQL' -v) > "$REPORT/tests.log" 2>&1 &&        (cd "$ROOT" && GOCACHE="${GOCACHE:-/tmp/eino-go-cache}" go run ./cmd/db-migrate -config "$ROOT/configs/worker/config.yaml" -profile worker -dry-run) > "$REPORT/dry-run.json" 2>> "$REPORT/tests.log" &&        grep -q '"mode": "dry-run"' "$REPORT/dry-run.json" &&        grep -q 'OpenWithoutMigration' "$ROOT/internal/platform/storage/entx/database.go" &&        ! grep -q 'WithDropColumn\|WithDropIndex' "$ROOT/internal/platform/storage/entx/database.go"; then
      cat "$REPORT/tests.log"
      cat > "$REPORT/summary.json" <<EOF
{"plan":"P27","mode":"local","status":"passed","checks":["migration SQL is checked in and embedded","dry-run performs no database connection or DDL","migration ledger and idempotent status API are implemented","application startup uses non-migrating database open path","destructive Ent drop options are absent from production connection path","real empty/history database upgrade and backup-restore remain environment-dependent"]}
EOF
      echo "PASS P27 local explicit-migration/startup-boundary contract; report: $REPORT"
    else
      status=$?; cat "$REPORT/tests.log" 2>/dev/null || true; echo "P27 local migration contract failed; report: $REPORT" >&2; exit "$status"
    fi
    ;;

  P28)
    [ "$#" -le 2 ] || { echo 'usage: P28 [--local]' >&2; exit 2; }
    [ "${2:-}" = '' ] || [ "${2:-}" = '--local' ] || { echo 'usage: P28 [--local]' >&2; exit 2; }
    REPORT="$ROOT/logs/acceptance/P28/$(date +%Y%m%d-%H%M%S)-$$-local"
    mkdir -p "$REPORT"
    if (cd "$ROOT" && GOCACHE="${GOCACHE:-/tmp/eino-go-cache}" go test -count=1 ./internal/indexversion -run 'TestShadowBuildSwitchAndRollbackAreGated' -v) > "$REPORT/tests.log" 2>&1 &&        (cd "$ROOT" && GOCACHE="${GOCACHE:-/tmp/eino-go-cache}" go run ./cmd/index-version -state "$REPORT/state.json" -command create -id shadow-v2 -embedding embed-v2 -dimensions 1024 -mapping map-v2 -expected 0) >> "$REPORT/tests.log" 2>&1 &&        grep -q 'shadow-v2' "$REPORT/state.json"; then
      cat "$REPORT/tests.log"
      cat > "$REPORT/summary.json" <<EOF
{"plan":"P28","mode":"local","status":"passed","checks":["shadow version is isolated by id/model/dimensions/mapping","build progress is durable","incomplete version cannot become ready","default active version is unchanged until explicit switch","CLI exposes status/create operations","real ES/Milvus shadow rebuild remains environment-dependent"]}
EOF
      echo "PASS P28 local shadow-index control-plane contract; report: $REPORT"
    else
      status=$?; cat "$REPORT/tests.log"; echo "P28 local shadow-index contract failed; report: $REPORT" >&2; exit "$status"
    fi
    ;;
  P29)
    [ "$#" -le 2 ] || { echo 'usage: P29 [--local]' >&2; exit 2; }
    [ "${2:-}" = '' ] || [ "${2:-}" = '--local' ] || { echo 'usage: P29 [--local]' >&2; exit 2; }
    REPORT="$ROOT/logs/acceptance/P29/$(date +%Y%m%d-%H%M%S)-$$-local"
    mkdir -p "$REPORT"
    if (cd "$ROOT" && GOCACHE="${GOCACHE:-/tmp/eino-go-cache}" go test -count=1 ./internal/indexversion -run 'TestShadowBuildSwitchAndRollbackAreGated|TestFailedVersionCannotBeActivated' -v) > "$REPORT/tests.log" 2>&1; then
      cat "$REPORT/tests.log"
      cat > "$REPORT/summary.json" <<EOF
{"plan":"P29","mode":"local","status":"passed","checks":["only ready versions can be switched","failed versions cannot be activated","active version cannot be marked failed","state survives reload and repeated status checks","rollback uses an existing ready version without deleting it","multi-instance and real index cutover remain environment-dependent"]}
EOF
      echo "PASS P29 local switch/rollback safety contract; report: $REPORT"
    else
      status=$?; cat "$REPORT/tests.log"; echo "P29 local switch/rollback contract failed; report: $REPORT" >&2; exit "$status"
    fi
    ;;

  P20)
    [ "$#" -le 2 ] || { echo 'usage: P20 [--local]' >&2; exit 2; }
    [ "${2:-}" = '' ] || [ "${2:-}" = '--local' ] || { echo 'usage: P20 [--local]' >&2; exit 2; }
    REPORT="$ROOT/logs/acceptance/P20/$(date +%Y%m%d-%H%M%S)-$$-local"
    mkdir -p "$REPORT"
    if (cd "$ROOT" && go test -count=1 ./internal/eval ./cmd/rag-test -v) > "$REPORT/tests.log" 2>&1; then
      cat "$REPORT/tests.log"
      cat > "$REPORT/summary.json" <<EOF
{"plan":"P20","mode":"local","status":"passed","checks":["JSONL dataset validation","threshold loading and gate violations","runner metrics and failure accounting","detail report rendering","live PostgreSQL/Milvus/ES baseline not run"]}
EOF
      echo "PASS P20 local evaluation contract; report: $REPORT"
    else
      status=$?; cat "$REPORT/tests.log"; echo "P20 local evaluation contract failed; report: $REPORT" >&2; exit "$status"
    fi
    ;;
  P21)
    [ "$#" -le 2 ] || { echo 'usage: P21 [--local]' >&2; exit 2; }
    [ "${2:-}" = '' ] || [ "${2:-}" = '--local' ] || { echo 'usage: P21 [--local]' >&2; exit 2; }
    REPORT="$ROOT/logs/acceptance/P21/$(date +%Y%m%d-%H%M%S)-$$-local"
    mkdir -p "$REPORT"
    if (cd "$ROOT" && go test -count=1 ./internal/eval ./internal/tool -run 'TestPrivacyDatasetHasNonEmptyIsolationCases|TestPrivacyRunnerFailsOnCrossSubjectLeak|TestKnowledgeSearchUsesIdentityBoundDatasetsAndFormatsCitations|TestKnowledgeSearchRequiresIdentityForDynamicBindings|TestKnowledgeSearchReturnsEmptyMessageWithoutAuthorizedDatasets|TestKnowledgeSearchResolvesBindingsOnEveryInvocation' -v) > "$REPORT/tests.log" 2>&1; then
      cat "$REPORT/tests.log"
      cat > "$REPORT/summary.json" <<EOF
{"plan":"P21","mode":"local","status":"passed","checks":["non-empty synthetic private corpus","positive subject-to-source cases","forbidden source detection","dynamic binding refresh","ACL leak count is non-zero on injected leak"]}
EOF
      echo "PASS P21 local privacy isolation contract; report: $REPORT"
    else
      status=$?; cat "$REPORT/tests.log"; echo "P21 local privacy isolation contract failed; report: $REPORT" >&2; exit "$status"
    fi
    ;;
  P22)
    [ "$#" -le 2 ] || { echo 'usage: P22 [--local]' >&2; exit 2; }
    [ "${2:-}" = '' ] || [ "${2:-}" = '--local' ] || { echo 'usage: P22 [--local]' >&2; exit 2; }
    REPORT="$ROOT/logs/acceptance/P22/$(date +%Y%m%d-%H%M%S)-$$-local"
    mkdir -p "$REPORT"
    if (cd "$ROOT" && go test -count=1 ./internal/eval -run 'TestEvaluateAnswerRequiresFactsAndAuthorizedCitation|TestEvaluateAnswerRejectsMissingOrUnauthorizedEvidence|TestEvaluateAnswerRejectsForbiddenFact|TestEvaluateAnswerRequiresExplicitAbstention' -v) > "$REPORT/tests.log" 2>&1; then
      cat "$REPORT/tests.log"
      cat > "$REPORT/summary.json" <<EOF
{"plan":"P22","mode":"local","status":"passed","checks":["required facts","forbidden facts","explicit no-answer abstention","citation parsing","authorized citation sources"]}
EOF
      echo "PASS P22 local answer evidence contract; report: $REPORT"
    else
      status=$?; cat "$REPORT/tests.log"; echo "P22 local answer evidence contract failed; report: $REPORT" >&2; exit "$status"
    fi
    ;;
  P23)
    [ "$#" -le 2 ] || { echo 'usage: P23 [--local]' >&2; exit 2; }
    [ "${2:-}" = '' ] || [ "${2:-}" = '--local' ] || { echo 'usage: P23 [--local]' >&2; exit 2; }
    REPORT="$ROOT/logs/acceptance/P23/$(date +%Y%m%d-%H%M%S)-$$-local"
    mkdir -p "$REPORT"
    exec "$ROOT/scripts/quality-gate.sh" "$REPORT"
    ;;
  P12)
    [ "$#" -le 2 ] || { echo 'usage: P12 [--local]' >&2; exit 2; }
    [ "${2:-}" = '' ] || [ "${2:-}" = '--local' ] || { echo 'usage: P12 [--local]' >&2; exit 2; }
    REPORT="$ROOT/logs/acceptance/P12/$(date +%Y%m%d-%H%M%S)-$$-local"
    mkdir -p "$REPORT"
    if (cd "$ROOT" && go test -count=1 ./internal/transport/restapi/internal/handler/dataset -run '^TestDocumentHTTPContract$') > "$REPORT/tests.log" 2>&1; then
      cat "$REPORT/tests.log"
      cat > "$REPORT/summary.json" <<EOF
{"plan":"P12","mode":"local","status":"passed","checks":["multiple file fields","empty file","unsupported format","per-file size limit","request total limit","max file count","partial success ledger","multipart spill cleanup"]}
EOF
      echo "PASS P12 local upload contract; report: $REPORT"
    else
      status=$?; cat "$REPORT/tests.log"; echo "P12 local upload contract failed; report: $REPORT" >&2; exit "$status"
    fi
    ;;
  P04)
    shift
    if [ "${1:-}" = '--local' ]; then
      [ "$#" -eq 1 ] || { echo '--local accepts no other options' >&2; exit 2; }
      REPORT="$ROOT/logs/acceptance/P04/$(date +%Y%m%d-%H%M%S)-$$-local"
      mkdir -p "$REPORT"
      if (cd "$ROOT" && make test-knowledge-lifecycle) > "$REPORT/tests.log" 2>&1; then
        cat "$REPORT/tests.log"; echo "PASS P04 local regression (not live E2E); report: $REPORT"
      else
        status=$?; cat "$REPORT/tests.log"; echo "P04 local regression failed; report: $REPORT" >&2; exit "$status"
      fi
    else
      exec python3 "$ROOT/scripts/acceptance/p04.py" "$@"
    fi
    ;;
  *) echo "BLOCKED: unsupported plan ${1:-}; run --list" >&2; exit 2 ;;
esac
