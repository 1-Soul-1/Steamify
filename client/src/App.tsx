import React, { useEffect, useState } from 'react'
import { login, me, register, type User } from './api'
import { Chat } from './Chat'
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

  // ЕСЛИ ЕСТЬ СОХРАНЁННЫЕ ТОКЕНЫ, ТО ПРОВЕРЯЕМ ИХ ЧЕРЕЗ /me
  useEffect(() => {
    if (!token) {
      setUser(null)
      return
    }
    me()
      .then(setUser)
      .catch(() => {
        localStorage.removeItem('token')
        setToken(null)
      })
  }, [token])

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
    localStorage.removeItem("token")
    setToken(null)
    setUser(null)
    setPassword("")
  }

  if (token && !user) return <div>Проверяем токен на уникальность.</div>

  if (token && user) return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '20px', alignItems: 'center' }}>
      <div className="card" style={{ textAlign: 'center' }}>
        <h1>Вы в системе</h1>
        <p>Id: {user.id}</p>
        <p>Email: {user.email}</p>
        <button onClick={log_out}>Выйти</button>
      </div>
      
      {/* ДОБАВЛЯЕМ ЧАТ */}
      <Chat token={token} />
    </div>
  )

  return (
    <>
      <form action="" onSubmit={submit}>
        <h1>Streamify</h1>
        <div>
          <button
          type='button'
          className={mode === "login" ? "active":""}
          onClick={() => {setMode('login'); setError(""); setNotice("");}}
          >Вход
          </button>

          <button
          type='button'
          className={mode === "register" ? "active":""}
          onClick={() => {setMode('register'); setError(""); setNotice("");}}
          >Регистрация
          </button>
        </div>

        <label htmlFor="">Email</label>
        <input type="email" required value={email} onChange={(e) => setEmail(e.target.value)} placeholder='Введите email' />

        <label htmlFor="">Пароль</label>
        <input type="password" required minLength={8} value={password} onChange={(e) =>setPassword(e.target.value)} placeholder='Введите пароль' />

        <button className='submit' disabled={busy}>{busy ? "Подождите...": mode === "login" ? "Войти": "Создать аккаунт"}</button>

        {error && <div>{error}</div>}
        {notice && !error && <div>{notice}</div>}

      </form>
    </>
  )
}

export default App
