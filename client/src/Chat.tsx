import React, { useEffect, useRef, useState } from 'react';

type Message = {
  type?: "chat" | "system"; // Добавили тип
  username?: string;
  text: string;
  time: string;
};

export const Chat = ({ token }: { token: string }) => {
  const [messages, setMessages] = useState<Message[]>([]);
  const [input, setInput] = useState("");
  const ws = useRef<WebSocket | null>(null);
  const bottomRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    ws.current = new WebSocket(`${protocol}//${window.location.host}/go/ws?token=${token}`);

    ws.current.onmessage = (event) => {
      const msg = JSON.parse(event.data);
      setMessages((prev) => [...prev, msg]);
    };

    ws.current.onerror = (e) => console.error("WebSocket error:", e);

    return () => {
      ws.current?.close();
    };
  }, [token]);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [messages]);

  const sendMessage = (e: React.FormEvent) => {
    e.preventDefault();
    if (!input.trim() || !ws.current) return;
    
    ws.current.send(JSON.stringify({ text: input }));
    setInput("");
  };

  return (
    <div className="chat-box">
      <h2 style={{ marginBottom: '10px' }}>Чат Стрима</h2>
      
      <div style={{ 
        flex: 1, 
        overflowY: 'auto', 
        overflowX: 'hidden',
        display: 'flex', 
        flexDirection: 'column', 
        gap: '10px', 
        marginBottom: '15px',
        paddingRight: '5px' 
      }}>
        {messages.map((msg, i) => {
          // Если это системное сообщение (вход/выход) — рендерим по-другому
          if (msg.type === "system") {
            return (
              <div key={i} style={{ 
                textAlign: 'center', 
                fontSize: '13px', 
                color: 'var(--text)', 
                fontStyle: 'italic',
                margin: '5px 0',
                opacity: 0.8
              }}>
                {msg.text}
              </div>
            );
          }

          // Обычное сообщение от пользователя
          return (
            <div key={i} style={{ fontSize: '14px', wordBreak: 'break-word', width: '100%' }}>
              <div style={{ display: 'flex', alignItems: 'baseline', gap: '8px' }}>
                <span style={{ color: 'var(--accent)', fontWeight: 'bold' }}>{msg.username}</span>
                <span style={{ color: 'var(--text)', fontSize: '12px' }}>{msg.time}</span>
              </div>
              <p style={{ margin: '2px 0 0 0', color: 'var(--text-h)', overflowWrap: 'anywhere' }}>
                {msg.text}
              </p>
            </div>
          );
        })}
        <div ref={bottomRef} />
      </div>

      <form onSubmit={sendMessage} style={{ display: 'flex', gap: '8px', padding: 0, background: 'none', border: 'none', boxShadow: 'none' }}>
        <input 
          type="text" 
          value={input} 
          onChange={(e) => setInput(e.target.value)} 
          placeholder="Написать сообщение..." 
          style={{ flex: 1 }}
        />
        <button type="submit" style={{ width: 'auto', padding: '0 16px' }}>➤</button>
      </form>
    </div>
  );
};
