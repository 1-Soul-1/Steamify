import { useEffect, useRef, useState } from 'react';
import Hls from 'hls.js';
import { startTestStream, stopTestStream } from './api';

export const Stream = ({ hasCamera }: { hasCamera: boolean | null }) => {
  const videoRef = useRef<HTMLVideoElement>(null);
  const [error, setError] = useState(true);
  const [isStreaming, setIsStreaming] = useState(false);

  useEffect(() => {
    const video = videoRef.current;
    if (!video || !isStreaming) {
      if (video) video.pause();
      return;
    }

    const src = 'http://127.0.0.1:8080/hls/stream.m3u8';
    let hls: Hls | null = null;
    let retryTimeout: ReturnType<typeof setTimeout>;

    const initHls = () => {
      if (Hls.isSupported()) {
        hls = new Hls({ 
          liveSyncDurationCount: 2, 
          lowLatencyMode: true,
          manifestLoadingMaxRetry: 1,
          levelLoadingMaxRetry: 1,
          fragLoadingMaxRetry: 1
        });
        hls.loadSource(src);
        hls.attachMedia(video);
        
        hls.on(Hls.Events.ERROR, (_e, data) => {
          if (data.fatal) {
            setError(true);
            retryTimeout = setTimeout(() => {
              hls?.destroy();
              initHls();
            }, 3000);
          }
        });

        hls.on(Hls.Events.MANIFEST_PARSED, () => {
          setError(false);
          video.play().catch(() => {});
        });
      } else if (video.canPlayType('application/vnd.apple.mpegurl')) {
        video.src = src;
        video.addEventListener('error', () => setError(true));
      }
    };

    initHls();

    return () => {
      if (hls) hls.destroy();
      clearTimeout(retryTimeout);
    };
  }, [isStreaming]);

  const handleStart = async () => {
    try {
      // Отправляем запрос на Go-сервер для старта (тестового) стрима
      await startTestStream();
      setIsStreaming(true);
      setError(true); // Ждем, пока HLS файлы появятся
    } catch (e) {
      alert("Не удалось запустить стрим на сервере. Проверьте, запущен ли Go и FFmpeg.");
    }
  };

  const handleStop = async () => {
    try {
      await stopTestStream();
      setIsStreaming(false);
      setError(true);
    } catch (e) {
      alert("Ошибка при остановке стрима.");
    }
  };

  return (
    <div style={{ width: '100%', maxWidth: '900px', display: 'flex', flexDirection: 'column', gap: '10px' }}>
      
      {/* Предупреждение о камере */}
      {hasCamera === false && (
        <div className="error" style={{ background: 'rgba(239, 68, 68, 0.1)', border: '1px solid #ef4444', borderRadius: '8px', padding: '12px', textAlign: 'center' }}>
          ⚠️ Камера не подключена или доступ к ней запрещен. Начать стрим невозможно.
        </div>
      )}

      {/* Окно видео */}
      <div style={{ position: 'relative' }}>
        {(!isStreaming || error) && (
          <div style={{
            position: 'absolute', top: 0, left: 0, right: 0, bottom: 0,
            background: '#111', borderRadius: '12px', border: '1px solid var(--border)',
            display: 'flex', alignItems: 'center', justifyContent: 'center',
            color: 'var(--text)', fontSize: '18px', flexDirection: 'column', gap: '10px',
            zIndex: 10, aspectRatio: '16/9'
          }}>
            <span style={{ fontSize: '40px' }}>{isStreaming ? "⏳" : "📴"}</span>
            <span>{isStreaming ? "Загрузка потока..." : "Стрим не активен"}</span>
          </div>
        )}
        <video
          ref={videoRef}
          autoPlay
          muted
          controls
          style={{ 
            width: '100%', background: '#000', borderRadius: '12px', 
            border: '1px solid var(--border)', display: 'block',
            aspectRatio: '16/9', objectFit: 'contain'
          }}
        />
      </div>

      {/* Кнопки управления */}
      <div style={{ display: 'flex', gap: '10px', justifyContent: 'center' }}>
        <button 
          onClick={handleStart} 
          disabled={!hasCamera || isStreaming}
          style={{ 
            background: (!hasCamera || isStreaming) ? '#333' : '#22c55e', 
            color: (!hasCamera || isStreaming) ? '#888' : '#fff',
            cursor: (!hasCamera || isStreaming) ? 'not-allowed' : 'pointer',
            flex: 1, maxWidth: '200px'
          }}
        >
          🟢 Начать стрим
        </button>
        
        <button 
          onClick={handleStop} 
          disabled={!isStreaming}
          style={{ 
            background: isStreaming ? '#ef4444' : '#333', 
            color: isStreaming ? '#fff' : '#888',
            cursor: isStreaming ? 'pointer' : 'not-allowed',
            flex: 1, maxWidth: '200px'
          }}
        >
          🔴 Закончить стрим
        </button>
      </div>
    </div>
  );
};