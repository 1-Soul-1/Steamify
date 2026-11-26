import React, { useEffect, useState } from 'react'
import { login, me, register, startTestStream, stopTestStream, type User } from './api'
import { Chat } from './Chat'
import { Stream } from './Stream'
import './App.css'

type Mode = "login" | "register"

function App() {
  const [mode, setMode] = useState<Mode>("login")
  const [email, setEmail] = useState("")
  const [password, setPassword] = useState("")
  const [token, setToken] = useState<string | null>(() => localStorage.getItem("token"))
  const [user, setUser] = useState<User | null>(null)
  const [error, setError] = useState("")
  const [notice, setNotice] = useState("")
  const [busy, setBusy] = useState(false)

  // Состояния для стрима и камеры
  const [hasCamera, setHasCamera] = useState<boolean | null>(null)
  const [isLive, setIsLive] = useState(false)

  useEffect(() => {
    if (!token) {
      setUser(null)
      return
    }
    me().then(setUser).catch(() => {
      localStorage.removeItem('token')
      setToken(null)
    })
  }, [token])

  // Проверка камеры
  useEffect(() => {
    navigator.mediaDevices.getUserMedia({ video: true })
      .then((stream) => {
        setHasCamera(true)
        stream.getTracks().forEach(track => track.stop()) // Сразу выключаем камеру
      })
      .catch(() => {
        setHasCamera(false)
      })
  }, [])

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError("")
    setNotice("")
    setBusy(true)
    try {
      if (mode === "register") {
        await register(email, password)
        setNotice("Аккаунт создан. Вход в систему.")
      }
      const { access_token } = await login(email, password)
      localStorage.setItem("token", access_token)
      setToken(access_token)
    } catch (err) {
      setError(err instanceof Error ? err.message : "Произошла ошибка.")
    } finally {
      setBusy(false)
    }
  }

  const log_out = () => {
    if (isLive) handleStopStream()
    localStorage.removeItem("token")
    setToken(null)
    setUser(null)
    setPassword("")
  }

  // Управление стримом
  const handleStartStream = async () => {
    try {
      await startTestStream()
      setTimeout(() => setIsLive(true), 2000)
    } catch (e) {
      setError("Не удалось запустить стрим. Проверьте подключение.")
    }
  }

  const handleStopStream = async () => {
    await stopTestStream()
    setIsLive(false)
  }

  if (token && !user) return <div>Проверяем токен...</div>

  // --- Интерфейс после входа ---
  if (token && user) return (
    <div className="app-layout">
      
      {/* ЛЕВАЯ КОЛОНКА: Профиль, Кнопки, Чат */}
      <div className="sidebar">
        <div className="card" style={{ textAlign: 'center', padding: '20px' }}>
          <h2>Вы в системе</h2>
          <p style={{ marginBottom: '10px', fontSize: '14px' }}>Email: {user.email}</p>
          
          {/* ПРЕДУПРЕЖДЕНИЕ О КАМЕРЕ */}
          {hasCamera === false && (
            <div className="error" style={{ marginBottom: '15px', background: 'var(--border)', color: 'var(--text-h)', fontSize: '13px' }}>
              ⚠️ Камера не подключена или доступ к ней запрещен. Начать стрим невозможно.
            </div>
          )}

          {/* КНОПКИ СТРИМА */}
          <div style={{ display: 'flex', gap: '10px', justifyContent: 'center', marginBottom: '10px' }}>
            {!isLive ? (
              <button onClick={handleStartStream} style={{ background: '#22c55e', color: '#fff' }}>
                🟢 Начать стрим
              </button>
            ) : (
              <button onClick={handleStopStream} style={{ background: '#ef4444', color: '#fff' }}>
                🔴 Закончить стрим
              </button>
            )}
          </div>
          
          <button onClick={log_out} className="secondary">Выйти</button>
        </div>
        
        <Chat token={token} />
      </div>
      
      {/* ПРАВАЯ КОЛОНКА: Видео */}
      <div className="main-content">
        <Stream isLive={isLive} />
      </div>

    </div>
  )

  // --- Интерфейс Входа/Регистрации ---
  return (
    <>
      <form onSubmit={submit}>
        <h1>Streamify</h1>
        <div style={{ display: 'flex', gap: '10px' }}>
          <button type='button' className={mode === "login" ? "active" : ""} onClick={() => { setMode('login'); setError(""); setNotice(""); }}>Вход</button>
          <button type='button' className={mode === "register" ? "active" : ""} onClick={() => { setMode('register'); setError(""); setNotice(""); }}>Регистрация</button>
        </div>
        <label>Email</label>
        <input type="email" required value={email} onChange={(e) => setEmail(e.target.value)} placeholder='Введите email' />
        <label>Пароль</label>
        <input type="password" required minLength={8} value={password} onChange={(e) => setPassword(e.target.value)} placeholder='Введите пароль' />
        <button className='submit' disabled={busy}>{busy ? "Подождите..." : mode === "login" ? "Войти" : "Создать аккаунт"}</button>
        {error && <div className="error">{error}</div>}
        {notice && !error && <div style={{ color: 'var(--accent)', textAlign: 'center' }}>{notice}</div>}
      </form>
    </>
  )
}

export default App