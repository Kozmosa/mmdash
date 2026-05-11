export type LocalAgentConnectionState =
  | "idle"
  | "connecting"
  | "ready"
  | "degraded"
  | "closed";

export interface LocalAgentErrorShape {
  code: string;
  message: string;
  retryable?: boolean;
  details?: Record<string, unknown>;
}

export class LocalAgentError extends Error {
  code: string;
  retryable: boolean;
  details: Record<string, unknown>;
  requestId?: string;

  constructor(
    message: string,
    options: {
      code: string;
      retryable?: boolean;
      details?: Record<string, unknown>;
      requestId?: string;
    }
  ) {
    super(message);
    this.name = "LocalAgentError";
    this.code = options.code;
    this.retryable = options.retryable ?? false;
    this.details = options.details ?? {};
    this.requestId = options.requestId;
  }
}

type PendingRequest = {
  resolve: (data: any) => void;
  reject: (error: LocalAgentError) => void;
  timeoutId: ReturnType<typeof setTimeout>;
};

type ResponseMessage = {
  request_id?: string;
  action?: string;
  ok?: boolean;
  protocol_version?: number;
  data?: any;
  error?: LocalAgentErrorShape;
};

const WS_URL = "ws://127.0.0.1:8765";
const REQUEST_TIMEOUT_MS = 30000;
const HEARTBEAT_INTERVAL_MS = 15000;
const HEARTBEAT_TIMEOUT_MS = 5000;
const REQUIRED_PROTOCOL_VERSION = 2;

let ws: WebSocket | null = null;
let reqId = 0;
let connectionState: LocalAgentConnectionState = "idle";
let connectPromise: Promise<WebSocket> | null = null;
let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
let reconnectAttempts = 0;
let heartbeatTimer: ReturnType<typeof setInterval> | null = null;
let protocolVersion: number | null = null;
const pendingRequests = new Map<string, PendingRequest>();
const stateListeners = new Set<(state: LocalAgentConnectionState) => void>();

function setConnectionState(nextState: LocalAgentConnectionState) {
  connectionState = nextState;
  stateListeners.forEach((listener) => listener(nextState));
}

function clearHeartbeat() {
  if (heartbeatTimer) {
    clearInterval(heartbeatTimer);
    heartbeatTimer = null;
  }
}

function rejectPendingRequests(code: string, message: string, retryable = true) {
  for (const [requestId, pending] of pendingRequests) {
    clearTimeout(pending.timeoutId);
    pending.reject(
      new LocalAgentError(message, {
        code,
        retryable,
        requestId,
      })
    );
  }
  pendingRequests.clear();
}

function scheduleReconnect() {
  if (reconnectTimer || connectionState === "closed") {
    return;
  }

  const delay = Math.min(1000 * 2 ** reconnectAttempts, 10000);
  reconnectAttempts += 1;
  reconnectTimer = setTimeout(() => {
    reconnectTimer = null;
    void connectLocalAgent().catch(() => {
      if (ws === null) {
        setConnectionState("degraded");
        scheduleReconnect();
      }
    });
  }, delay);
}

function handleMessage(event: MessageEvent) {
  let payload: ResponseMessage;
  try {
    payload = JSON.parse(event.data);
  } catch {
    return;
  }

  const requestId = payload.request_id;
  if (!requestId) {
    return;
  }

  if (typeof payload.protocol_version === "number") {
    protocolVersion = payload.protocol_version;
  }

  const pending = pendingRequests.get(requestId);
  if (!pending) {
    return;
  }

  clearTimeout(pending.timeoutId);
  pendingRequests.delete(requestId);

  if (payload.ok === false) {
    pending.reject(
      new LocalAgentError(payload.error?.message || "Local Agent request failed", {
        code: payload.error?.code || "agent_error",
        retryable: payload.error?.retryable,
        details: payload.error?.details,
        requestId,
      })
    );
    return;
  }

  pending.resolve(payload.data);
}

function startHeartbeat() {
  clearHeartbeat();
  heartbeatTimer = setInterval(() => {
    if (!ws || ws.readyState !== WebSocket.OPEN) {
      return;
    }

    void sendAction("ping", undefined, { timeoutMs: HEARTBEAT_TIMEOUT_MS, suppressReconnect: true }).catch(
      () => {
        setConnectionState("degraded");
        ws?.close();
      }
    );
  }, HEARTBEAT_INTERVAL_MS);
}

function bindSocket(socket: WebSocket, resolve: (value: WebSocket) => void, reject: (reason?: unknown) => void) {
  socket.onopen = () => {
    ws = socket;
    reconnectAttempts = 0;
    setConnectionState("ready");
    startHeartbeat();
    resolve(socket);
  };

  socket.onerror = () => {
    if (connectPromise) {
      reject(
        new LocalAgentError("Failed to connect to Local Agent", {
          code: "connection_failed",
          retryable: true,
        })
      );
    }
  };

  socket.onclose = () => {
    const wasReady = connectionState === "ready";
    ws = null;
    protocolVersion = null;
    clearHeartbeat();
    connectPromise = null;
    if (connectionState !== "closed") {
      setConnectionState(wasReady ? "degraded" : "idle");
      rejectPendingRequests("connection_closed", "Local Agent connection closed");
      scheduleReconnect();
    }
  };

  socket.onmessage = handleMessage;
}

export function getLocalAgentState(): LocalAgentConnectionState {
  return connectionState;
}

export function subscribeLocalAgentState(listener: (state: LocalAgentConnectionState) => void) {
  stateListeners.add(listener);
  return () => {
    stateListeners.delete(listener);
  };
}

export function isConnected(): boolean {
  return ws !== null && ws.readyState === WebSocket.OPEN && connectionState === "ready";
}

export function disconnectLocalAgent() {
  setConnectionState("closed");
  if (reconnectTimer) {
    clearTimeout(reconnectTimer);
    reconnectTimer = null;
  }
  clearHeartbeat();
  rejectPendingRequests("connection_closed", "Local Agent disconnected", false);
  connectPromise = null;
  if (ws) {
    ws.close();
    ws = null;
  }
}

export function connectLocalAgent(): Promise<WebSocket> {
  if (ws && ws.readyState === WebSocket.OPEN) {
    return Promise.resolve(ws);
  }
  if (connectPromise) {
    return connectPromise;
  }

  setConnectionState("connecting");
  connectPromise = new Promise<WebSocket>((resolve, reject) => {
    const socket = new WebSocket(WS_URL);
    bindSocket(socket, resolve, reject);
  }).finally(() => {
    connectPromise = null;
  });

  return connectPromise as Promise<WebSocket>;
}

async function ensureProtocol() {
  if (protocolVersion === REQUIRED_PROTOCOL_VERSION) {
    return;
  }

  const info = await sendAction("agent.info", undefined, {
    timeoutMs: HEARTBEAT_TIMEOUT_MS,
    suppressReconnect: true,
  });
  const version = info?.protocol_version;
  if (version !== REQUIRED_PROTOCOL_VERSION) {
    throw new LocalAgentError("Local Agent protocol version mismatch", {
      code: "protocol_mismatch",
      details: { expected: REQUIRED_PROTOCOL_VERSION, actual: version ?? null },
    });
  }
  protocolVersion = version;
}

export async function sendAction(
  action: string,
  params?: any,
  options?: { timeoutMs?: number; suppressReconnect?: boolean }
): Promise<any> {
  if (!ws || ws.readyState !== WebSocket.OPEN) {
    await connectLocalAgent();
  }

  if (action !== "agent.info") {
    await ensureProtocol();
  }

  const requestId = `${++reqId}`;
  const timeoutMs = options?.timeoutMs ?? REQUEST_TIMEOUT_MS;

  return new Promise((resolve, reject) => {
    const timeoutId = setTimeout(() => {
      pendingRequests.delete(requestId);
      reject(
        new LocalAgentError("Local Agent request timeout", {
          code: "request_timeout",
          retryable: true,
          requestId,
          details: { action, timeoutMs },
        })
      );
    }, timeoutMs);

    pendingRequests.set(requestId, {
      resolve,
      reject,
      timeoutId,
    });

    try {
      ws!.send(JSON.stringify({ request_id: requestId, action, params }));
    } catch {
      clearTimeout(timeoutId);
      pendingRequests.delete(requestId);
      reject(
        new LocalAgentError("Failed to send request to Local Agent", {
          code: "send_failed",
          retryable: true,
          requestId,
          details: { action },
        })
      );
      if (!options?.suppressReconnect) {
        setConnectionState("degraded");
        ws?.close();
      }
    }
  });
}
