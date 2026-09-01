/* eslint-disable react-refresh/only-export-components */
import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react';
import { get, post } from '../lib/api';
import type { User } from '../lib/types';

interface AuthValue {
  user: User | null;
  loading: boolean;
  login: (username: string, password: string) => Promise<void>;
  logout: () => Promise<void>;
  refresh: () => Promise<void>;
}

const AuthContext = createContext<AuthValue | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null);
  const [loading, setLoading] = useState(true);

  const refresh = useCallback(async () => {
    try {
      const session = await get<{ authenticated: boolean; user?: User }>('/session');
      setUser(session.authenticated && session.user ? session.user : null);
    } catch {
      setUser(null);
    } finally { setLoading(false); }
  }, []);

  useEffect(() => { void refresh(); }, [refresh]);
  useEffect(() => {
    const unauthorized = () => setUser(null);
    window.addEventListener('jikim:unauthorized', unauthorized);
    return () => window.removeEventListener('jikim:unauthorized', unauthorized);
  }, []);

  const login = useCallback(async (username: string, password: string) => {
    const result = await post<{ user: User }>('/auth/login', { username, password });
    if (!result.user) throw new Error('서버가 로그인 사용자 정보를 반환하지 않았습니다.');
    setUser(result.user);
  }, []);

  const logout = useCallback(async () => {
    try { await post('/auth/logout'); } catch { /* local session is always cleared */ }
    setUser(null);
  }, []);

  const value = useMemo(() => ({ user, loading, login, logout, refresh }), [user, loading, login, logout, refresh]);
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth() {
  const value = useContext(AuthContext);
  if (!value) throw new Error('AuthProvider 바깥에서 useAuth를 사용할 수 없습니다.');
  return value;
}
