import { useEffect, useState } from 'react';
import { Alert, Center, Loader, Stack, Text, Title } from '@mantine/core';
import { Navigate, useNavigate, useSearchParams } from 'react-router-dom';
import { post } from '../lib/api';
import { clearSilentSsoState, safeReturnTo } from '../lib/silentSso';
import { useAuth } from '../contexts/AuthContext';

export function OidcCallbackPage() {
  const [params] = useSearchParams();
  const navigate = useNavigate();
  const { user, refresh } = useAuth();
  const code = params.get('code');
  // 깊은 링크로 들어와 로그인한 사람은 그 자리로 돌아간다. 같은 오리진 경로만 받는다.
  const returnTo = safeReturnTo(params.get('return_to'));
  const providerError = params.get('error_description') || params.get('error');
  const [error, setError] = useState(() => providerError || (!code ? 'OIDC 로그인 교환 코드가 없습니다.' : ''));
  useEffect(() => {
    if (providerError || !code) return;
    void (async () => {
      try {
        await post('/oidc/exchange', { code });
        clearSilentSsoState();
        await refresh();
        navigate(returnTo, { replace: true });
      } catch (caught) { setError(caught instanceof Error ? caught.message : 'SSO 로그인에 실패했습니다.'); }
    })();
  }, [code, providerError, navigate, refresh, returnTo]);
  if (user) return <Navigate to={returnTo} replace />;
  return <Center mih="100vh" bg="gray.0"><Stack align="center" maw={520} p="xl" ta="center">{error ? <><Title order={1}>SSO 로그인을 완료하지 못했습니다.</Title><Alert color="red">{error}</Alert><Text c="dimmed">로그인 화면으로 돌아가 다시 시도하거나 서비스 관리자에게 Keycloak 설정을 확인해 달라고 요청하세요.</Text></> : <><Loader size="lg" /><Title order={2}>Keycloak 로그인을 확인하고 있습니다.</Title><Text c="dimmed">이 창을 닫거나 새로 고치지 마세요.</Text></>}</Stack></Center>;
}
