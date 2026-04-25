export interface User {
  id: string;
  name: string;
}

export interface FetchClientOptions extends RequestInit {
  baseUrl?: string;
  fetcher?: typeof fetch;
}

export type FetchClient = <TResponse>(
  path: string,
  options?: FetchClientOptions
) => Promise<TResponse>;

export type ChatRole = 'user' | 'assistant';

export interface ChatTranscriptMessage {
  id: string;
  role: ChatRole;
  content: string;
  time: string;
  actions?: string[];
}

export type QuickAction = 'help' | 'status' | 'tasks' | 'agents' | 'approvals';

export interface HomelabdMessageRequest {
  from?: string;
  content: string;
}

export interface HomelabdMessageResponse {
  reply: string;
}

export interface HomelabdClient {
  sendMessage(request: HomelabdMessageRequest): Promise<HomelabdMessageResponse>;
}

export interface HomelabdClientOptions {
  baseUrl?: string;
  fetcher?: typeof fetch;
}
