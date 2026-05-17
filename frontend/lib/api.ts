import axios from "axios";
import { clearAuthCookie } from "@/lib/auth-cookie";

const api = axios.create({
  baseURL: "/api",
  headers: {
    "Content-Type": "application/json",
  },
});

api.interceptors.request.use((config) => {
  const token = localStorage.getItem("token");
  if (token && !config.headers.Authorization) {
    config.headers.Authorization = `Bearer ${token}`;
  }
  return config;
});

api.interceptors.response.use(
  (response) => response,
  (error) => {
    if (error.response?.status === 401) {
      localStorage.removeItem("token");
      clearAuthCookie();
      window.location.href = "/auth/login";
    }
    return Promise.reject(error);
  }
);

// LLM API endpoints
export const llmApi = {
  async getProviders() {
    const res = await api.get("/llm/providers");
    return res.data.providers;
  },

  async getCurrentBinding(providerType?: string, teamId?: string) {
    const res = await api.get("/llm/binding/current", {
      params: {
        ...(providerType ? { provider_type: providerType } : {}),
        ...(teamId ? { team_id: teamId } : {}),
      },
    });
    return res.data.binding;
  },

  async getModels(bindingId?: string, provider?: string, teamId?: string) {
    const res = await api.get("/llm/models", {
      params: {
        ...(bindingId ? { binding_id: bindingId } : {}),
        ...(provider ? { provider } : {}),
        ...(teamId ? { team_id: teamId } : {}),
      },
    });
    return res.data.models;
  },

  async createBinding(
    providerType: string,
    credentials: Record<string, string>,
    teamId?: string
  ) {
    const res = await api.post("/llm/bindings", {
      provider_type: providerType,
      credentials,
      team_id: teamId,
    });
    return res.data;
  },

  async selectModel(bindingId: string, selectedModel: string) {
    const res = await api.post("/llm/selection", {
      binding_id: bindingId,
      selected_model: selectedModel,
    });
    return res.data;
  },

  async getPromptSettings(teamId: string) {
    const res = await api.get("/llm/prompts", {
      params: { team_id: teamId },
    });
    return res.data;
  },

  async updatePromptSettings(teamId: string, prompts: Record<string, string>) {
    const res = await api.put("/llm/prompts", {
      team_id: teamId,
      prompts,
    });
    return res.data;
  },
};

// Reminder API endpoints
export const reminderApi = {
  async getPending(projectId: string) {
    const res = await api.get(`/reminders/${projectId}/pending`);
    return res.data as {
      events: { id: string; title: string; description: string; start_time: string }[];
      todos: { id: string; content: string; due_date: string | null }[];
    };
  },

  async ack(projectId: string, type: "event" | "todo", ids: string[]) {
    const res = await api.post(`/reminders/${projectId}/ack`, { type, ids });
    return res.data;
  },
};

// IM notification API endpoints
export const imApi = {
  async getStatus() {
    const res = await api.get("/im/status");
    return res.data as { providers: { type: string; configured: boolean; name: string }[] };
  },

  async getUserBinding() {
    const res = await api.get("/im/user-binding");
    return res.data as { binding: { id: string; provider_type: string; im_user_id: string; enabled: boolean } | null };
  },

  async saveUserBinding(data: { provider_type: string; im_user_id: string; enabled: boolean }) {
    const res = await api.post("/im/user-binding", data);
    return res.data;
  },

  async getProjectBinding(projectId: string) {
    const res = await api.get(`/im/project-binding/${projectId}`);
    return res.data as { binding: { id: string; provider_type: string; im_chat_id: string; enabled: boolean } | null };
  },

  async saveProjectBinding(projectId: string, data: { provider_type: string; im_chat_id: string; enabled: boolean }) {
    const res = await api.post(`/im/project-binding/${projectId}`, data);
    return res.data;
  },

  async verify(data: { provider_type: string; recipient_type: string; recipient_id: string }) {
    const res = await api.post("/im/verify", data);
    return res.data as { success: boolean; error?: string };
  },
};

export default api;
