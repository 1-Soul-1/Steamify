import axios, { AxiosError } from "axios"

export const api = axios.create({ baseURL: `/api` })

api.interceptors.request.use((config) => {
    const token = localStorage.getItem("token");
    if(token) config.headers.Authorization = `Bearer ${token}`;
    return config
})

api.interceptors.response.use((res) => res,
    (error:AxiosError<{detail:unknown}>) => {
        if(!error.response) return Promise.reject(new Error("Backend недоступен."))
        const detail = error.response.data?.detail;
        let message = `Ошибка: ${error.response.status}`;
        if(typeof detail == "string") message = detail;
        else if(Array.isArray(detail)) message = detail.map((e:{msg:string}) => e.msg).join(';');
        return Promise.reject(new Error(message))
    }
);

export type User = {id:number; email:string};
export type Token = {access_token:string, token_type:string}

export const register = (email:string, password:string) =>
    api.post<User>("/register",{email, password}).then((r) => r.data);

export const login = (email:string, password:string) =>
    api.post<Token>("/login", new URLSearchParams({username:email, password})).then((r) => r.data);

export const me = () => api.get<User>("/me").then((r) => r.data);

// --- Управление стримами ---
export const getStreams = () => 
    fetch('/go/api/streams').then(r => r.json());

export const startTestStream = (room: string) =>
    fetch(`/go/api/stream/start?room=${encodeURIComponent(room)}`, { method: "POST" }).then(r => r.json());

export const stopTestStream = (room: string) =>
    fetch(`/go/api/stream/stop?room=${encodeURIComponent(room)}`, { method: "POST" }).then(r => r.json());
