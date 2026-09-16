/**
 * gocliRequest.ts —— goctl 生成文件的 axios 版本（手写，非生成产物）。
 *
 * `goctl api ts` 每次都会重新覆盖本文件，所以它被刻意写成**零依赖**：
 * 只依赖 axios（peer 依赖，需由接入方 npm i axios），不 import 任何项目内模块，
 * 这样重新生成后手工替换回来即可，不需要改一行业务代码。
 * Makefile 的 gen-ts 已改为「生成到临时目录 + 只覆盖 restapi.ts /
 * restapiComponents.ts」，本文件不会再被生成器冲掉。
 *
 * ── 为什么不用生成器原版（裸 fetch）────────────────────────────────
 * 1. GET/DELETE 会带上请求体。原实现是
 *      body: data ? JSON.stringify(data) : undefined
 *    `{}` 在 JS 里是真值，而 restapi.ts 里带 path 参数的 GET 恰好传 `{}`
 *    （DatasetIDReqParams 这类 Params 类型都是空接口）。浏览器 fetch 对
 *    GET/HEAD + body 直接抛
 *      TypeError: Request with GET/HEAD method cannot have body
 *    也就是 getDataset / listKnowledgeDocuments 在最原始的版本里根本跑不起来。
 * 2. 没有鉴权。后端 /dataset* 与 /agents/:subject/dataset* 全部挂 RoleAdmin，
 *    必须带 Bearer key，原版连 Authorization 都不注入。
 * 3. 错误不抛。原版无论状态码一律 response.json()，4xx/5xx 时调用方拿到的是
 *    错误信封 {code, error, request_id}（见 restapi/internal/httpx），却按成功
 *    响应类型读字段，字段全 undefined，问题被静默吞掉。
 * 4. 只认 JSON。文件上传需要 multipart，原版把 body 写死成 JSON.stringify。
 *
 * ── 这一层做了什么 ────────────────────────────────────────────────
 * 保持 `webapi.{get,post,put,delete,patch}` 的签名与 genUrl 的拼接行为不变
 * （restapi.ts 因此一行都不用改），底层换成 axios 实例，并补齐：
 *   - 请求拦截：Accept / Content-Type（FormData 时交给浏览器自己写 boundary）/ Bearer
 *   - 响应拦截：把 {code,error,request_id} 与常见状态码归一成可上屏的 message
 *   - 读请求（GET/DELETE）参数走 query，绝不带 body
 *   - 额外导出 http（裸实例）与 upload（multipart），补上生成器装不下的场景
 *
 * ── 用法 ─────────────────────────────────────────────────────────
 *   import webapi, { configureHttp, setAuthToken, upload, errorMessage }
 *       from "./gocliRequest"
 *
 *   // 入口处配置一次即可；不配置就是「同源 + 30s 超时 + 读 localStorage.access_token」
 *   configureHttp({
 *       baseURL: import.meta.env.VITE_API_BASE_URL ?? "",
 *       getToken: () => localStorage.getItem("access_token"),
 *   })
 *
 *   const kb = await listDatasets()                       // 走拦截器，自动带上 Bearer
 *   const form = new FormData(); form.append("file", file)
 *   await upload<components.StatusResp>(`/api/v1/dataset/${id}/documents`, form)
 *   catch (e) { message = errorMessage(e, "上传失败") }
 *
 * ── 生成器已知的另一个坑（本层有意不修）──────────────────────────
 * restapi.ts 里 decideApproval 是
 *   webapi.post(url, params, req)
 * 而 webapi.post 的签名是 (url, req, config) —— 第三个位置是 axios 配置，
 * 不是请求体。于是「审批决策」的 body 会被当成 axios config 丢掉，实际发出
 * 空 body。这一层不做「猜第三个参数到底是 body 还是 config」的魔法，接审批
 * 时请改成 `webapi.post(url, req)` 或直接用 http 实例发。
 */
import axios, {
    type AxiosError,
    type AxiosInstance,
    type AxiosRequestConfig,
    type InternalAxiosRequestConfig,
} from "axios"

export type Method =
    | "get"
    | "GET"
    | "delete"
    | "DELETE"
    | "head"
    | "HEAD"
    | "options"
    | "OPTIONS"
    | "post"
    | "POST"
    | "put"
    | "PUT"
    | "patch"
    | "PATCH"

/* ──────────────────────────── 可配置项 ──────────────────────────── */

export interface HttpClientConfig {
    /** 接口前缀。默认 ""（同源）——生成的路由已带 /api/v1，这里不要再叠一层 */
    baseURL?: string
    /** 超时毫秒，默认 30000 */
    timeout?: number
    /** 需要跨站携带 Cookie 时置 true，默认 false */
    withCredentials?: boolean
    /** 每次请求前取值作为 Bearer；返回空则不带 Authorization */
    getToken?: () => string | null | undefined
    /** 401 回调，便于上层跳登录 */
    onUnauthorized?: (error: AxiosError<ApiErrorBody>) => void
}

/** 后端统一错误信封，见 restapi/internal/httpx */
export interface ApiErrorBody {
    code?: string
    error?: string
    message?: string
    request_id?: string
}

const ERROR_MESSAGE_BY_STATUS: Record<number, string> = {
    400: "请求参数有误",
    401: "登录已过期或凭证无效，请重新登录",
    403: "当前账号没有该资源的操作权限",
    404: "资源不存在或已被删除",
    413: "文件过大，超出服务端限制",
    429: "请求过于频繁，请稍后重试",
    500: "服务内部错误，请稍后重试",
    503: "服务暂时不可用，请稍后重试",
}

/** 浏览器环境下默认读 localStorage.access_token；非浏览器环境返回空 */
function defaultGetToken(): string | null | undefined {
    try {
        return typeof localStorage === "undefined"
            ? undefined
            : localStorage.getItem("access_token")
    } catch {
        return undefined
    }
}

let runtimeConfig: HttpClientConfig = {
    baseURL: "",
    timeout: 30000,
    withCredentials: false,
    getToken: defaultGetToken,
}

/** 代码里显式设定的 token，优先级高于 getToken */
let staticToken: string | null = null

/** 在应用入口调用一次即可；也可只传局部字段做覆盖 */
export function configureHttp(config: HttpClientConfig): void {
    runtimeConfig = { ...runtimeConfig, ...config }

    http.defaults.baseURL = runtimeConfig.baseURL ?? ""
    http.defaults.timeout = runtimeConfig.timeout ?? 30000
    http.defaults.withCredentials = runtimeConfig.withCredentials ?? false
}

/**
 * 登录 / 登出时设置 token。
 * setAuthToken(null) 表示清除；清除后回退到 configureHttp 的 getToken。
 */
export function setAuthToken(token: string | null): void {
    staticToken = token
}

/* ───────────────────────────── axios 实例 ──────────────────────────── */

export const http: AxiosInstance = axios.create({
    baseURL: runtimeConfig.baseURL ?? "",
    timeout: runtimeConfig.timeout ?? 30000,
    withCredentials: runtimeConfig.withCredentials ?? false,
})

http.interceptors.request.use(
    (config: InternalAxiosRequestConfig) => {
        config.headers.set("Accept", "application/json")

        // FormData 交给浏览器自动补 multipart boundary，手写反而会坏
        if (!(config.data instanceof FormData)) {
            config.headers.set("Content-Type", "application/json")
        }

        const token = staticToken ?? runtimeConfig.getToken?.()
        if (token) {
            config.headers.set("Authorization", `Bearer ${token}`)
        }

        return config
    },
    (error: AxiosError) => Promise.reject(error),
)

http.interceptors.response.use(
    (response) => response,
    (error: AxiosError<ApiErrorBody>) => {
        const status = error.response?.status
        const body = error.response?.data
        const serverMessage = body?.error || body?.message

        if (status === 401) {
            runtimeConfig.onUnauthorized?.(error)
        }

        if (serverMessage) {
            error.message = serverMessage
        } else if (status && ERROR_MESSAGE_BY_STATUS[status]) {
            error.message = ERROR_MESSAGE_BY_STATUS[status]
        } else if (error.code === "ECONNABORTED") {
            error.message = "请求超时，请稍后重试"
        } else if (!error.response) {
            error.message = "网络异常，请检查网络连接"
        }

        return Promise.reject(error)
    },
)

/** 把任意 catch 到的错误转成可以直接上屏的文案 */
export function errorMessage(error: unknown, fallback = "操作失败，请稍后重试"): string {
    if (error instanceof Error && error.message) {
        return error.message
    }

    return fallback
}

/* ─────────────────────── 与生成器一致的 url 处理 ─────────────────────── */

/** 只匹配未插值的 `:name` 占位符 */
const reg = /:[a-z|A-Z]+/g

/**
 * Parse route parameters for responseType
 */
export function parseParams(url: string): Array<string> {
    const ps = url.match(reg)
    if (!ps) {
        return []
    }
    return ps.map((k) => k.replace(/:/, ""))
}

/**
 * Generate url and parameters
 * @param url
 * @param params
 */
export function genUrl(url: string, params: any): string {
    if (!params) {
        return url
    }

    const ps = parseParams(url)
    ps.forEach((k) => {
        const reg = new RegExp(`:${k}`)
        url = url.replace(reg, String(params[k]))
    })

    const path: Array<string> = []
    for (const key of Object.keys(params)) {
        if (ps.find((k) => k === key)) {
            continue
        }

        const value = params[key]
        if (value === undefined || value === null || value === "") {
            continue
        }

        path.push(`${key}=${encodeURIComponent(String(value))}`)
    }

    return url + (path.length > 0 ? `?${path.join("&")}` : "")
}

/**
 * 把「既不是 params/forms 包装、也不是 FormData」的普通对象合并进查询串。
 *
 * 生成器把 Params 对象同时当 body 传（见文件头第 1 条），这些对象在
 * restapi.ts 里都是空接口，正常情况下这里无事可做；留这条是为了把调用方
 * 显式传入的查询条件也带上，而不是塞进 GET 的 body。
 */
function appendQuery(url: string, req: any): string {
    if (
        !req ||
        typeof req !== "object" ||
        Array.isArray(req) ||
        req instanceof FormData ||
        "params" in req ||
        "forms" in req
    ) {
        return url
    }

    const query: Array<string> = []
    for (const key of Object.keys(req)) {
        const value = req[key]
        if (value === undefined || value === null || value === "") {
            continue
        }
        query.push(`${key}=${encodeURIComponent(String(value))}`)
    }

    if (query.length === 0) {
        return url
    }

    return url + (url.includes("?") ? "&" : "?") + query.join("&")
}

/* ─────────────────────────────── 请求入口 ─────────────────────────────── */

/**
 * 与生成器同名同签名，只是换成 axios 并返回 T（原版返回 any，且错误不抛）。
 * 需要透传 axios 配置（responseType、signal、onUploadProgress…）时用 config。
 */
export async function request<T>({
    method,
    url,
    data,
    config = {},
}: {
    method: Method
    url: string
    data?: unknown
    config?: unknown
}): Promise<T> {
    const response = await http.request<T>({
        method,
        url,
        data,
        ...(config as AxiosRequestConfig),
    })

    return response.data
}

function api<T>(method: Method = "get", url: string, req: any, config?: unknown): Promise<T> {
    const isRead = /get|delete/i.test(method)

    if (url.match(/:/) || isRead) {
        url = genUrl(url, req?.params || req?.forms)
    }

    // 读请求一律不带 body：XHR 会忽略 GET 的 body，但 fetch 会直接抛
    // 「Request with GET/HEAD method cannot have body」。生成器正是把 Params
    // 对象当 body 传的，所以这里显式置空，把参数改走 query。
    const data = isRead ? undefined : req
    if (isRead) {
        url = appendQuery(url, req)
    }

    method = method.toLocaleLowerCase() as Method

    switch (method) {
        case "get":
            return request<T>({ method: "get", url, data, config })
        case "delete":
            return request<T>({ method: "delete", url, data, config })
        case "put":
            return request<T>({ method: "put", url, data, config })
        case "post":
            return request<T>({ method: "post", url, data, config })
        case "patch":
            return request<T>({ method: "patch", url, data, config })
        default:
            return request<T>({ method: "post", url, data, config })
    }
}

/* ─────────────────────────────── 对外出口 ─────────────────────────────── */

export const webapi = {
    get<T>(url: string, req?: unknown, config?: unknown): Promise<T> {
        return api<T>("get", url, req, config)
    },
    delete<T>(url: string, req?: unknown, config?: unknown): Promise<T> {
        return api<T>("delete", url, req, config)
    },
    put<T>(url: string, req?: unknown, config?: unknown): Promise<T> {
        return api<T>("put", url, req, config)
    },
    post<T>(url: string, req?: unknown, config?: unknown): Promise<T> {
        return api<T>("post", url, req, config)
    },
    patch<T>(url: string, req?: unknown, config?: unknown): Promise<T> {
        return api<T>("patch", url, req, config)
    },
}

/**
 * multipart 上传。生成器只会发 JSON body，装不下文件，所以单独给一个入口，
 * 走的是同一个 axios 实例（鉴权、错误归一、超时都复用）。
 *
 *   const form = new FormData()
 *   form.append("file", file)
 *   await upload<StatusResp>(`/api/v1/dataset/${id}/documents`, form, {
 *       onUploadProgress: (e) => (percent = Math.round((e.loaded / e.total!) * 100)),
 *   })
 */
export function upload<T>(url: string, data: FormData | Blob, config?: AxiosRequestConfig): Promise<T> {
    return http.post<T>(url, data, config).then((response) => response.data)
}

export default webapi
