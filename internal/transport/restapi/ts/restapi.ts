import webapi from "./gocliRequest"
import * as components from "./restapiComponents"
export * from "./restapiComponents"

/**
 * @description 
 * @param params
 */
export function listAgentDatasets(params: components.AgentSubjectReqParams, subject: string) {
	return webapi.get<components.DatasetListResp>(`/api/v1/agents/${subject}/dataset`, params)
}

/**
 * @description 
 * @param params
 */
export function grantAgentDataset(params: components.AgentDatasetReqParams, subject: string, id: number) {
	return webapi.put<components.StatusResp>(`/api/v1/agents/${subject}/dataset/${id}`, params)
}

/**
 * @description 
 * @param params
 */
export function revokeAgentDataset(params: components.AgentDatasetReqParams, subject: string, id: number) {
	return webapi.delete<components.StatusResp>(`/api/v1/agents/${subject}/dataset/${id}`, params)
}

/**
 * @description 
 */
export function createSession() {
	return webapi.post<components.CreateSessionResp>(`/api/v1/sessions`)
}

/**
 * @description 
 * @param params
 */
export function getApproval(params: components.ApprovalPathReqParams, id: string) {
	return webapi.get<components.ApprovalResp>(`/api/v1/approvals/${id}`, params)
}

/**
 * @description 
 * @param params
 * @param req
 */
export function decideApproval(params: components.ApprovalDecisionReqParams, req: components.ApprovalDecisionReq, id: string) {
	return webapi.post<components.StatusResp>(`/api/v1/approvals/${id}/decision`, params, req)
}

/**
 * @description "创建数据集"
 * @param req
 */
export function createDataset(req: components.CreateDatasetReq) {
	return webapi.post<components.DatasetResp>(`/api/v1/dataset`, req)
}

/**
 * @description "列出数据集"
 */
export function listDatasets() {
	return webapi.get<components.DatasetListResp>(`/api/v1/dataset`)
}

/**
 * @description "获取数据集详情"
 * @param params
 */
export function getDataset(params: components.DatasetIDReqParams, id: number) {
	return webapi.get<components.DatasetResp>(`/api/v1/dataset/${id}`, params)
}

/**
 * @description "删除数据集"
 * @param params
 */
export function deleteDataset(params: components.DatasetIDReqParams, id: number) {
	return webapi.delete<components.StatusResp>(`/api/v1/dataset/${id}`, params)
}

/**
 * @description "创建文档"
 * @param params
 * @param req
 */
export function createDocument(params: components.CreateDocumentReqParams, req: components.CreateDocumentReq, id: number) {
	return webapi.post<components.DocumentResp>(`/api/v1/dataset/${id}/documents`, params, req)
}

/**
 * @description "列出数据集文档"
 * @param params
 */
export function listDocuments(params: components.DatasetIDReqParams, id: number) {
	return webapi.get<components.DocumentListResp>(`/api/v1/dataset/${id}/documents`, params)
}

/**
 * @description "获取文档详情"
 * @param params
 */
export function getDocument(params: components.DocumentIDReqParams, id: number, docId: number) {
	return webapi.get<components.DocumentResp>(`/api/v1/dataset/${id}/documents/${docId}`, params)
}

/**
 * @description "更新文档"
 * @param params
 * @param req
 */
export function updateDocument(params: components.UpdateDocumentReqParams, req: components.UpdateDocumentReq, id: number, docId: number) {
	return webapi.put<components.DocumentResp>(`/api/v1/dataset/${id}/documents/${docId}`, params, req)
}

/**
 * @description "删除文档"
 * @param params
 */
export function deleteDocument(params: components.DocumentIDReqParams, id: number, docId: number) {
	return webapi.delete<components.StatusResp>(`/api/v1/dataset/${id}/documents/${docId}`, params)
}

/**
 * @description "重建文档索引"
 * @param params
 */
export function reindexDocument(params: components.DocumentIDReqParams, id: number, docId: number) {
	return webapi.post<components.DocumentResp>(`/api/v1/dataset/${id}/documents/${docId}/reindex`, params)
}

/**
 * @description "重建数据集全部文档索引"
 * @param params
 */
export function reindexDataset(params: components.DatasetIDReqParams, id: number) {
	return webapi.post<components.ReindexResp>(`/api/v1/dataset/${id}/reindex`, params)
}

/**
 * @description 
 */
export function health() {
	return webapi.get<components.HealthResp>(`/health`)
}

/**
 * @description 
 */
export function ready() {
	return webapi.get<components.ReadyResp>(`/ready`)
}
