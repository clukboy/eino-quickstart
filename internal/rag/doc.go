// Package eino implements a RAG ingestion and retrieval pipeline natively on
// top of the CloudWeGo Eino component abstractions.
//
// Instead of hand-rolling loader / chunker / indexer / retriever orchestration,
// this package implements the Eino component interfaces
//
//	document.Loader      (FileLoader)
//	document.Transformer (MarkdownChunker)
//	embedding.Embedder   (adapter over an internal float32 embedder)
//	indexer.Indexer      (over a Store)
//	retriever.Retriever  (hybrid vector + lexical + RRF over a Store)
//
// and wires them together with the Eino compose primitives (Chain / Graph).
// The document "currency" that flows between stages is *schema.Document, whose
// MetaData carries provenance (source, title, heading path, chunk index).
//
// Production vector / metadata storage is injected through the [Store]
// interface. A [MemStore] is bundled so every component can be exercised in
// unit tests without external services (Milvus / PostgreSQL); swap in the real
// implementations behind [Store] when running against infrastructure.
package rag
