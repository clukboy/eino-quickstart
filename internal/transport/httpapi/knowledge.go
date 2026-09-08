package server

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"eino-quickstart/ent"
	"eino-quickstart/ent/agentknowledgebase"
	"eino-quickstart/ent/document"
	"eino-quickstart/ent/knowledgebase"
	"eino-quickstart/internal/knowledge"
	"eino-quickstart/internal/platform/auth"

	"github.com/goccy/go-json"
)

var supportedUploadExtensions = map[string]struct{}{
	".markdown": {},
	".md":       {},
	".text":     {},
	".txt":      {},
}

type createKnowledgeBaseRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Visibility  string `json:"visibility"`
}

type knowledgeBaseResponse struct {
	ID           uint64 `json:"id"`
	Name         string `json:"name"`
	Description  string `json:"description,omitempty"`
	OwnerSubject string `json:"owner_subject"`
	Visibility   string `json:"visibility"`
	Status       string `json:"status"`
}

type documentResponse struct {
	ID              uint64 `json:"id"`
	KnowledgeBaseID uint64 `json:"knowledge_base_id"`
	Source          string `json:"source"`
	Title           string `json:"title"`
	Status          string `json:"status"`
	ChunkCount      int    `json:"chunk_count"`
}

func (s *Server) createKnowledgeBase(w http.ResponseWriter, r *http.Request) {
	client, ok := s.knowledgeClient(w)
	if !ok {
		return
	}
	identity, ok := auth.IdentityFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	var request createKnowledgeBaseRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON request: " + err.Error()})
		return
	}
	name := strings.TrimSpace(request.Name)
	visibility := strings.ToLower(strings.TrimSpace(request.Visibility))
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "knowledge base name is required"})
		return
	}
	if visibility == "" {
		visibility = string(knowledgebase.VisibilityPrivate)
	}
	if visibility != string(knowledgebase.VisibilityPrivate) && visibility != string(knowledgebase.VisibilitySystem) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "knowledge base visibility is invalid"})
		return
	}
	base, err := client.KnowledgeBase.Create().
		SetName(name).
		SetDescription(strings.TrimSpace(request.Description)).
		SetOwnerSubject(identity.Subject).
		SetVisibility(knowledgebase.Visibility(visibility)).
		SetStatus(knowledgebase.StatusACTIVE).
		Save(r.Context())
	if err != nil {
		writeKnowledgeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, knowledgeBaseDTO(base))
}

func (s *Server) listKnowledgeBases(w http.ResponseWriter, r *http.Request) {
	client, ok := s.knowledgeClient(w)
	if !ok {
		return
	}
	bases, err := client.KnowledgeBase.Query().Order(knowledgebase.ByID()).All(r.Context())
	if err != nil {
		writeKnowledgeError(w, err)
		return
	}
	result := make([]knowledgeBaseResponse, 0, len(bases))
	for _, base := range bases {
		result = append(result, knowledgeBaseDTO(base))
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": result})
}

func (s *Server) getKnowledgeBase(w http.ResponseWriter, r *http.Request) {
	client, ok := s.knowledgeClient(w)
	if !ok {
		return
	}
	knowledgeBaseID, ok := knowledgeBaseIDFromPath(w, r)
	if !ok {
		return
	}
	base, err := client.KnowledgeBase.Get(r.Context(), knowledgeBaseID)
	if err != nil {
		writeKnowledgeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, knowledgeBaseDTO(base))
}

func (s *Server) uploadKnowledgeDocument(w http.ResponseWriter, r *http.Request) {
	client, ok := s.knowledgeClient(w)
	if !ok {
		return
	}
	if s.KnowledgeIngestor == nil || s.KnowledgeMaxDocumentBytes <= 0 {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "knowledge ingestion is unavailable"})
		return
	}
	knowledgeBaseID, ok := knowledgeBaseIDFromPath(w, r)
	if !ok {
		return
	}
	base, err := client.KnowledgeBase.Get(r.Context(), knowledgeBaseID)
	if ent.IsNotFound(err) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "knowledge base not found"})
		return
	}
	if err != nil {
		writeKnowledgeError(w, err)
		return
	}
	if base.Status != knowledgebase.StatusACTIVE {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "knowledge base is disabled"})
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "multipart field file is required"})
		return
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, int64(s.KnowledgeMaxDocumentBytes)+1))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "read uploaded file: " + err.Error()})
		return
	}
	source, err := uploadSource(header.Filename)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if len(content) == 0 || len(content) > s.KnowledgeMaxDocumentBytes {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("document size must be between 1 and %d bytes", s.KnowledgeMaxDocumentBytes)})
		return
	}
	if !utf8.Valid(content) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "knowledge document must be valid UTF-8"})
		return
	}
	metadata, err := uploadMetadata(r.FormValue("metadata"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	title := strings.TrimSpace(r.FormValue("title"))
	if title == "" {
		title = strings.TrimSuffix(source, filepath.Ext(source))
	}
	if err := s.KnowledgeIngestor.Ingest(r.Context(), knowledge.IngestRequest{
		Source: source, Title: title, Content: string(content),
		Target: knowledge.IngestTarget{
			KnowledgeBaseID: knowledgeBaseID, Metadata: metadata,
			OwnerSubject: base.OwnerSubject, Visibility: string(base.Visibility),
		},
	}); err != nil {
		writeKnowledgeError(w, err)
		return
	}
	doc, err := client.Document.Query().
		Where(document.KnowledgeBaseIDEQ(knowledgeBaseID), document.SourceEQ(source)).
		Only(r.Context())
	if err != nil {
		writeKnowledgeError(w, err)
		return
	}
	response, err := documentDTO(r, doc)
	if err != nil {
		writeKnowledgeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, response)
}

func (s *Server) listKnowledgeDocuments(w http.ResponseWriter, r *http.Request) {
	client, ok := s.knowledgeClient(w)
	if !ok {
		return
	}
	knowledgeBaseID, ok := knowledgeBaseIDFromPath(w, r)
	if !ok {
		return
	}
	if exists, err := client.KnowledgeBase.Query().Where(knowledgebase.IDEQ(knowledgeBaseID)).Exist(r.Context()); err != nil {
		writeKnowledgeError(w, err)
		return
	} else if !exists {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "knowledge base not found"})
		return
	}
	docs, err := client.Document.Query().
		Where(document.KnowledgeBaseIDEQ(knowledgeBaseID)).
		Order(document.ByID()).
		All(r.Context())
	if err != nil {
		writeKnowledgeError(w, err)
		return
	}
	result := make([]documentResponse, 0, len(docs))
	for _, doc := range docs {
		response, err := documentDTO(r, doc)
		if err != nil {
			writeKnowledgeError(w, err)
			return
		}
		result = append(result, response)
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": result})
}

func (s *Server) listAgentKnowledgeBases(w http.ResponseWriter, r *http.Request) {
	client, ok := s.knowledgeClient(w)
	if !ok {
		return
	}
	subject, ok := agentSubjectFromPath(w, r)
	if !ok {
		return
	}
	bindings, err := client.AgentKnowledgeBase.Query().
		Where(agentknowledgebase.SubjectEQ(subject)).
		Order(agentknowledgebase.ByKnowledgeBaseID()).
		WithKnowledgeBase().
		All(r.Context())
	if err != nil {
		writeKnowledgeError(w, err)
		return
	}
	result := make([]knowledgeBaseResponse, 0, len(bindings))
	for _, binding := range bindings {
		base, err := binding.Edges.KnowledgeBaseOrErr()
		if err != nil {
			writeKnowledgeError(w, err)
			return
		}
		result = append(result, knowledgeBaseDTO(base))
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": result})
}

func (s *Server) grantAgentKnowledgeBase(w http.ResponseWriter, r *http.Request) {
	client, ok := s.knowledgeClient(w)
	if !ok {
		return
	}
	identity, authenticated := auth.IdentityFromContext(r.Context())
	if !authenticated {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	subject, ok := agentSubjectFromPath(w, r)
	if !ok {
		return
	}
	knowledgeBaseID, ok := knowledgeBaseIDFromPath(w, r)
	if !ok {
		return
	}
	if exists, err := client.KnowledgeBase.Query().
		Where(knowledgebase.IDEQ(knowledgeBaseID), knowledgebase.StatusEQ(knowledgebase.StatusACTIVE)).
		Exist(r.Context()); err != nil {
		writeKnowledgeError(w, err)
		return
	} else if !exists {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "active knowledge base not found"})
		return
	}
	if exists, err := client.AgentKnowledgeBase.Query().
		Where(agentknowledgebase.SubjectEQ(subject), agentknowledgebase.KnowledgeBaseIDEQ(knowledgeBaseID)).
		Exist(r.Context()); err != nil {
		writeKnowledgeError(w, err)
		return
	} else if exists {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := client.AgentKnowledgeBase.Create().
		SetSubject(subject).
		SetKnowledgeBaseID(knowledgeBaseID).
		SetCreatedBy(identity.Subject).
		Exec(r.Context()); err != nil {
		writeKnowledgeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) revokeAgentKnowledgeBase(w http.ResponseWriter, r *http.Request) {
	client, ok := s.knowledgeClient(w)
	if !ok {
		return
	}
	subject, ok := agentSubjectFromPath(w, r)
	if !ok {
		return
	}
	knowledgeBaseID, ok := knowledgeBaseIDFromPath(w, r)
	if !ok {
		return
	}
	deleted, err := client.AgentKnowledgeBase.Delete().
		Where(agentknowledgebase.SubjectEQ(subject), agentknowledgebase.KnowledgeBaseIDEQ(knowledgeBaseID)).
		Exec(r.Context())
	if err != nil {
		writeKnowledgeError(w, err)
		return
	}
	if deleted == 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "knowledge base binding not found"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) knowledgeClient(w http.ResponseWriter) (*ent.Client, bool) {
	if s.KnowledgeClient == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "knowledge management is unavailable"})
		return nil, false
	}
	return s.KnowledgeClient, true
}

func knowledgeBaseIDFromPath(w http.ResponseWriter, r *http.Request) (uint64, bool) {
	id, err := strconv.ParseUint(r.PathValue("id"), 10, 64)
	if err != nil || id == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "knowledge base id is invalid"})
		return 0, false
	}
	return id, true
}

func agentSubjectFromPath(w http.ResponseWriter, r *http.Request) (string, bool) {
	subject := strings.TrimSpace(r.PathValue("subject"))
	if subject == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "agent subject is required"})
		return "", false
	}
	return subject, true
}

func uploadMetadata(raw string) (knowledge.Metadata, error) {
	if strings.TrimSpace(raw) == "" {
		return knowledge.NewMetadata(nil), nil
	}
	var values map[string]any
	if err := json.Unmarshal([]byte(raw), &values); err != nil || values == nil {
		return knowledge.Metadata{}, errors.New("metadata must be a JSON object")
	}
	return knowledge.NewMetadata(values), nil
}

func uploadSource(filename string) (string, error) {
	filename = strings.TrimSpace(filename)
	if filename == "" || filename != filepath.Base(filename) || strings.Contains(filename, `\`) {
		return "", errors.New("knowledge document filename is invalid")
	}
	if _, supported := supportedUploadExtensions[strings.ToLower(filepath.Ext(filename))]; !supported {
		return "", errors.New("knowledge document format is not supported")
	}
	return filename, nil
}

func documentDTO(r *http.Request, doc *ent.Document) (documentResponse, error) {
	chunkCount, err := doc.QueryChunks().Count(r.Context())
	if err != nil {
		return documentResponse{}, fmt.Errorf("count document chunks: %w", err)
	}
	return documentResponse{
		ID: doc.ID, KnowledgeBaseID: doc.KnowledgeBaseID, Source: doc.Source,
		Title: doc.Title, Status: string(doc.Status), ChunkCount: chunkCount,
	}, nil
}

func knowledgeBaseDTO(base *ent.KnowledgeBase) knowledgeBaseResponse {
	return knowledgeBaseResponse{
		ID: base.ID, Name: base.Name, Description: base.Description,
		OwnerSubject: base.OwnerSubject, Visibility: string(base.Visibility), Status: string(base.Status),
	}
}

func writeKnowledgeError(w http.ResponseWriter, err error) {
	switch {
	case ent.IsNotFound(err):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "knowledge resource not found"})
	case ent.IsConstraintError(err):
		writeJSON(w, http.StatusConflict, map[string]string{"error": "knowledge resource already exists"})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "knowledge operation failed"})
	}
}
