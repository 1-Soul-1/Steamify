import { useEffect, useRef, useState } from 'react';
import Hls from 'hls.js';

export const Stream = ({ isLive }: { isLive: boolean }) => {
  const videoRef = useRef<HTMLVideoElement>(null);
  const [error, setError] = useState(false);

  useEffect(() => {
    const video = videoRef.current;
    if (!video || !isLive) {
      setError(false); // Сбрасываем ошибку при выключении
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
  }, [isLive]);

  return (
    <div style={{ width: '100%', height: '100%', position: 'relative', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
      {(!isLive || error) && (
        <div style={{
          position: 'absolute', top: 0, left: 0, right: 0, bottom: 0,
          background: '#111',
          display: 'flex', alignItems: 'center', justifyContent: 'center',
          color: 'var(--text)', fontSize: '18px', flexDirection: 'column', gap: '10px',
          zIndex: 10
        }}>
          <span style={{ fontSize: '50px' }}>📴</span>
          <span>{isLive ? "Ожидание сигнала от сервера..." : "Стрим не активен"}</span>
        </div>
      )}
      <video
        ref={videoRef}
        autoPlay
        muted
        controls
        style={{ 
          width: '100%', 
          height: '100%', 
          maxHeight: '100%',
          maxWidth: '100%',
          background: '#000', 
          display: 'block',
          objectFit: 'contain'
        }}
      />
    </div>
  );
};