import { Center, Loader, Stack, Text } from '@mantine/core';
import { Navigate, Outlet, Route, Routes, useLocation } from 'react-router-dom';
import { AuthProvider, useAuth } from './contexts/AuthContext';
import { AppLayout } from './components/AppLayout';
import { LoginPage } from './pages/LoginPage';
import { OidcCallbackPage } from './pages/OidcCallbackPage';
import { DashboardPage } from './pages/DashboardPage';
import { SecretsPage } from './pages/secrets/SecretsPage';
import { SecretEditorPage } from './pages/secrets/SecretEditorPage';
import { SecretDetailPage } from './pages/secrets/SecretDetailPage';
import { ApplicationsPage } from './pages/applications/ApplicationsPage';
import { ApprovalsPage } from './pages/ApprovalsPage';
import { AuditPage } from './pages/AuditPage';
import { KeysPage } from './pages/KeysPage';
import { AiPage } from './pages/AiPage';
import { ApiExplorerPage } from './pages/ApiExplorerPage';
import { FeaturePage } from './pages/FeaturePage';
import { AdminSettingsPage } from './pages/admin';
import { ProfilePage, PersonalKeysPage } from './pages/personal';
import { AuthenticationPage, IdentitiesPage, PoliciesPage, TokensPage } from './pages/access';
import { ForbiddenPage } from './pages/ForbiddenPage';
import { NotFoundPage } from './pages/NotFoundPage';

function ProtectedRoute() {
  const { user, loading } = useAuth();
  const location = useLocation();
  if (loading) return <Center mih="100vh"><Stack align="center"><Loader /><Text c="dimmed">안전한 세션을 확인하고 있습니다.</Text></Stack></Center>;
  if (!user) return <Navigate to="/login" state={{ from: location }} replace />;
  return <Outlet />;
}

function AdminRoute() {
  const { user } = useAuth();
  return user?.role === 'admin' ? <Outlet /> : <Navigate to="/forbidden" replace />;
}

function RolesRoute({ roles }: { roles: string[] }) {
  const { user } = useAuth();
  return user && roles.includes(user.role) ? <Outlet /> : <Navigate to="/forbidden" replace />;
}

export default function App() {
  return <AuthProvider><Routes>
    <Route path="/login" element={<LoginPage />} />
    <Route path="/oidc/callback" element={<OidcCallbackPage />} />
    <Route element={<ProtectedRoute />}>
      <Route element={<AppLayout />}>
        <Route index element={<Navigate to="/dashboard" replace />} />
        <Route path="dashboard" element={<DashboardPage />} />
        <Route path="secrets" element={<SecretsPage />} />
        <Route path="secrets/new" element={<SecretEditorPage />} />
        <Route path="secrets/dynamic" element={<FeaturePage feature="dynamic" />} />
        <Route path="secrets/leases" element={<FeaturePage feature="leases" />} />
        <Route path="secrets/rotation" element={<FeaturePage feature="rotation" />} />
        <Route path="secrets/risk" element={<FeaturePage feature="risk" />} />
        <Route path="secrets/:id" element={<SecretDetailPage />} />
        <Route path="encryption/transit" element={<FeaturePage feature="transit" />} />
        <Route element={<RolesRoute roles={['admin', 'manager']} />}>
          <Route path="encryption/keys" element={<KeysPage />} />
        </Route>
        <Route path="encryption/operations" element={<FeaturePage feature="crypto-operations" />} />
        <Route path="certificates" element={<FeaturePage feature="certificates" />} />
        <Route path="certificates/pki" element={<FeaturePage feature="pki" />} />
        <Route path="certificates/expiration" element={<FeaturePage feature="expiration" />} />
        <Route element={<RolesRoute roles={['admin', 'manager', 'auditor']} />}>
          <Route path="access/policies" element={<PoliciesPage />} />
          <Route path="access/identities" element={<IdentitiesPage />} />
        </Route>
        <Route path="access/tokens" element={<TokensPage />} />
        <Route path="approvals" element={<ApprovalsPage />} />
        <Route path="applications" element={<ApplicationsPage />} />
        <Route path="applications/environments" element={<FeaturePage feature="environments" />} />
        <Route path="applications/dependencies" element={<FeaturePage feature="dependencies" />} />
        <Route element={<RolesRoute roles={['admin', 'manager', 'auditor']} />}>
          <Route path="audit" element={<AuditPage />} />
          <Route path="audit/events" element={<FeaturePage feature="events" />} />
          <Route path="audit/security" element={<FeaturePage feature="security-events" />} />
        </Route>
        <Route path="ai" element={<AiPage />} />
        <Route path="api-explorer" element={<ApiExplorerPage />} />
        <Route path="personal/profile" element={<ProfilePage />} />
        <Route path="personal/keys" element={<PersonalKeysPage />} />
        <Route path="personal/preferences" element={<FeaturePage feature="preferences" />} />
        <Route path="guide" element={<FeaturePage feature="guide" />} />
        <Route path="forbidden" element={<ForbiddenPage />} />
        <Route element={<AdminRoute />}>
          <Route path="access/authentication" element={<AuthenticationPage />} />
          <Route path="infrastructure/cluster" element={<FeaturePage feature="cluster" />} />
          <Route path="infrastructure/storage" element={<FeaturePage feature="storage" />} />
          <Route path="infrastructure/security" element={<FeaturePage feature="infrastructure-security" />} />
          <Route path="admin/settings" element={<AdminSettingsPage />} />
          <Route path="admin/users" element={<IdentitiesPage adminMode />} />
          <Route path="admin/namespaces" element={<FeaturePage feature="namespaces" />} />
        </Route>
        <Route path="*" element={<NotFoundPage />} />
      </Route>
    </Route>
    <Route path="*" element={<Navigate to="/dashboard" replace />} />
  </Routes></AuthProvider>;
}
