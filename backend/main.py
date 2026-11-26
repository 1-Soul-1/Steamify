import os
import uvicorn
from contextlib import asynccontextmanager
from datetime import datetime, timedelta, timezone
from typing import Annotated

import jwt
from dotenv import load_dotenv
from fastapi import Depends, FastAPI, HTTPException, status
from fastapi.security import OAuth2PasswordBearer, OAuth2PasswordRequestForm
from jwt.exceptions import InvalidTokenError
from pwdlib import PasswordHash
from pydantic import BaseModel, ConfigDict, EmailStr, Field
from sqlalchemy import String, create_engine, select
from sqlalchemy.exc import IntegrityError
from sqlalchemy.orm import (DeclarativeBase, Mapped, Session, mapped_column, sessionmaker,)
from fastapi.middleware.cors import CORSMiddleware

load_dotenv()

# НАСТРОЙКИ КЛЮЧА, ENV, АЛГОРИТМА, ХЕШИРОВАНИЯ И ТОКЕНА
SECRET_KEY = os.environ["JWT_SECRET_KEY"]
ALGORITHM = "HS256"
ACCESS_TOKEN_EXPIRE_MINUTES = 30

password_hash = PasswordHash.recommended()
oauth2_scheme = OAuth2PasswordBearer(tokenUrl="login")

# ПОДКЛЮЧЕНИЕ И СОЗДАНИЕ БАЗЫ ДАННЫХ
DATABASE_URL = "sqlite:///./users.db"
engine = create_engine(
    DATABASE_URL, connect_args={"check_same_thread": False}
)
SessionLocal = sessionmaker(
    bind=engine,
    autoflush=False,
    expire_on_commit=False,
)

class Base(DeclarativeBase):
    pass

class User(Base):
    __tablename__ = "users"

    id: Mapped[int] = mapped_column(primary_key=True)
    email: Mapped[str] = mapped_column(String(320), unique=True, index=True)
    hashed_password: Mapped[str] = mapped_column(String(255))


def get_db():
    with SessionLocal() as db:
        yield db


DbSession = Annotated[Session, Depends(get_db)]



# PYDANTIC-СХЕМЫ
# ПРЕДСТАВЛЕНИЕ БАЗОВОЙ МОДЕЛИ
class UserCreate(BaseModel):
    email: EmailStr
    password: str = Field(
        min_length=8,
        max_length=128,
    )

class UserResponse(BaseModel):
    id: int
    email: EmailStr

    model_config = ConfigDict(from_attributes=True)

class TokenResponse(BaseModel):
    access_token: str
    token_type: str



# СОЗДАНИЕ JWT
def create_access_token(user_id: int, email: str) -> str:
    expire = datetime.now(timezone.utc) + timedelta(
        minutes=ACCESS_TOKEN_EXPIRE_MINUTES
    )
    payload = {
        "sub": str(user_id),
        "email": email,
        "exp": expire,
    }
    token = jwt.encode(payload, SECRET_KEY, algorithm=ALGORITHM)
    return token



# ПРОВЕРКА АВТОРИЗАЦИИ ПОЛЬЗОВАТЕЛЯ
def get_current_user(token: Annotated[str, Depends(oauth2_scheme)], db: Annotated[Session, Depends(get_db)],) -> User:
    error = HTTPException(
        status_code=status.HTTP_401_UNAUTHORIZED,
        detail="Недействительный токен или срок его действия истёк.",
        headers={"WWW-Authenticate": "Bearer"},
    )
    try:
        payload = jwt.decode(
            token,
            SECRET_KEY,
            algorithms=[ALGORITHM],
            options={"require": ["sub", "exp"]},
        )
        user_id = int(payload["sub"])
    except (InvalidTokenError, ValueError, TypeError):
        raise error from None

    user = db.get(User, user_id)
    if user is None:
        raise error
    return user

CurrentUser = Annotated[User, Depends(get_current_user)]



# ЗАПУСК ПРИЛОЖЕНИЯ
@asynccontextmanager
async def lifespan(app: FastAPI):
    Base.metadata.create_all(bind=engine)
    yield

app = FastAPI(
    title="Authentication API",
    version="1.0.0",
    lifespan=lifespan,
)


# ОТКРЫТЬ ВСЕ CORS
app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"]
)



@app.get("/")
def home():
    return {"message": "FastAPI работает."}


# РЕГИСТРАЦИЯ
@app.post(
    "/register",
    response_model=UserResponse,
    status_code=status.HTTP_201_CREATED,
)

def register(data: UserCreate, db: Annotated[Session, Depends(get_db)],):
    email = str(data.email).strip().lower()
    existing_user = db.scalar(select(User).where(User.email == email))
    if existing_user is not None:
        raise HTTPException(
            status_code=status.HTTP_409_CONFLICT,
            detail="Email уже существует.",
        )

    hashed_password = password_hash.hash(data.password)
    new_user = User(
        email=email,
        hashed_password=hashed_password,
    )
    db.add(new_user)
    try:
        db.commit()
    except IntegrityError:
        db.rollback()
        raise HTTPException(
            status_code=status.HTTP_409_CONFLICT,
            detail="Email уже зарегистрирован.",
        ) from None

    db.refresh(new_user)
    return new_user


# АВТОРИЗАЦИЯ
@app.post(
    "/login",
    response_model=TokenResponse,
)

def login(form: Annotated[OAuth2PasswordRequestForm, Depends()], db: Annotated[Session, Depends(get_db)]):
    email = form.username.strip().lower()
    user = db.scalar(select(User).where(User.email == email))
    
    if user is None:
        # ИСПРАВЛЕНО: Было form.password.password_hash.hash("dummy")
        password_hash.verify(form.password, password_hash.hash("dummy_password_123"))
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail="Неверный логин и/или пароль.",
            headers={"WWW-Authenticate": "Bearer"}
        )
    if not password_hash.verify(form.password, user.hashed_password):
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail="Неверный логин и/или пароль.",
            headers={"WWW-Authenticate": "Bearer"}
        )

    # ИСПРАВЛЕНО: передаем email в функцию
    token = create_access_token(user.id, user.email)
    return TokenResponse(
        access_token=token,
        token_type="bearer",
    )


# ЗАЩИЩЁННЫЙ МАРШРУТ
@app.get(
    "/me",
    response_model=UserResponse,
)

def get_profile(
    current_user: Annotated[User, Depends(get_current_user)],
):
    return current_user


if __name__ == "__main__":
    uvicorn.run("main:app", host="127.0.0.1", port=8000,reload=True)