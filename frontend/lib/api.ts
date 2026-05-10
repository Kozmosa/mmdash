import axios from "axios";

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

export default api;
