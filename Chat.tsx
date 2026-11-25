import React, { useEffect, useRef, useState } from 'react';

type Message = {
  username: string;
  text: string;
  time: string;
};

export const Chat = ({ token }: { token: string }) => {
  const [messages, setMessages] = useState<Message[]>([]);
  const [input, setInput] = useState("");
  const ws = useRef<WebSocket | null>(null);
  const bottomRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    // Подключаемся к Go-серверу, передавая токен
    ws.current = new WebSocket(`ws://127.0.0.1:8080/ws?token=${token}`);

    ws.current.onmessage = (event) => {
      const msg = JSON.parse(event.data);
      setMessages((prev) => [...prev, msg]);
    };

    ws.current.onerror = (e) => console.error("WebSocket error:", e);

    return () => {
      ws.current?.close();
    };
  }, [token]);

  // Автоскролл вниз
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
    <div style={{ display: 'flex', flexDirection: 'column', height: '400px', width: '100%', maxWidth: '500px', background: 'var(--card-bg)', padding: '16px', borderRadius: '12px', border: '1px solid var(--border)' }}>
      <h2 style={{ marginBottom: '10px' }}>Чат Стрима</h2>
      <div style={{ flex: 1, overflowY: 'auto', display: 'flex', flexDirection: 'column', gap: '8px', marginBottom: '10px' }}>
        {messages.map((msg, i) => (
          <div key={i} style={{ fontSize: '14px' }}>
            <span style={{ color: 'var(--accent)', fontWeight: 'bold' }}>{msg.username}</span>
            <span style={{ color: 'var(--text)', fontSize: '12px', marginLeft: '8px' }}>{msg.time}</span>
            <p style={{ margin: '2px 0 0 0', color: 'var(--text-h)' }}>{msg.text}</p>
          </div>
        ))}
        <div ref={bottomRef} />
      </div>
      <form onSubmit={sendMessage} style={{ display: 'flex', gap: '8px', padding: 0, background: 'none', border: 'none', boxShadow: 'none', maxWidth: '100%' }}>
        <input 
          type="text" 
          value={input} 
          onChange={(e) => setInput(e.target.value)} 
          placeholder="Написать сообщение..." 
        />
        <button type="submit">➤</button>
      </form>
    </div>
  );
};