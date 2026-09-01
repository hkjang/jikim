import { useEffect, useMemo, useState } from 'react';
import { AppShell, Avatar, Box, Burger, Button, Divider, Drawer, Group, Image, Menu, Modal, ScrollArea, Stack, Text, TextInput, ThemeIcon, Tooltip, UnstyledButton } from '@mantine/core';
import { useDisclosure } from '@mantine/hooks';
import { NavLink, Outlet, useLocation, useNavigate } from 'react-router-dom';
import {
  Activity, AppWindow, Bot, Box as BoxIcon, Boxes, Cable, ChevronDown, CircleUserRound, Clock3, Database,
  FileKey, FileSearch, GitBranch, KeyRound, LayoutDashboard, LifeBuoy, ListChecks, LockKeyhole,
  LogOut, Network, PanelLeftClose, Search, ServerCog, Settings, ShieldCheck, SlidersHorizontal, Sparkles,
  UserCog, Users, Workflow, X,
} from 'lucide-react';
import { useQuery } from '@tanstack/react-query';
import { get } from '../lib/api';
import { APP_VERSION } from '../lib/format';
import { useAuth } from '../contexts/AuthContext';

type NavItem = {
  label: string;
  path: string;
  icon: React.ComponentType<{ size?: number; strokeWidth?: number }>;
  admin?: boolean;
  approval?: boolean;
  roles?: string[];
};
type NavGroup = { label: string; items: NavItem[] };

const navGroups: NavGroup[] = [
  { label: '개요', items: [{ label: '대시보드', path: '/dashboard', icon: LayoutDashboard }] },
  { label: '시크릿', items: [
    { label: '시크릿 탐색기', path: '/secrets', icon: KeyRound },
    { label: '동적 자격 증명', path: '/secrets/dynamic', icon: Sparkles },
    { label: '리스', path: '/secrets/leases', icon: Clock3 },
    { label: '회전 관리', path: '/secrets/rotation', icon: Workflow },
    { label: '위험 분석', path: '/secrets/risk', icon: ShieldCheck },
  ] },
  { label: '암호화', items: [
    { label: 'Transit', path: '/encryption/transit', icon: LockKeyhole },
    { label: '키 관리', path: '/encryption/keys', icon: FileKey, roles: ['admin', 'manager'] },
    { label: '암호화 작업', path: '/encryption/operations', icon: SlidersHorizontal },
  ] },
  { label: '인증서', items: [
    { label: '인증서 현황', path: '/certificates', icon: ShieldCheck },
    { label: 'PKI·CA', path: '/certificates/pki', icon: Network },
    { label: '만료 관리', path: '/certificates/expiration', icon: Clock3 },
  ] },
  { label: '접근 제어', items: [
    { label: '인증 방식', path: '/access/authentication', icon: LockKeyhole, admin: true },
    { label: '정책', path: '/access/policies', icon: ListChecks, roles: ['admin', 'manager', 'auditor'] },
    { label: '사용자·Identity', path: '/access/identities', icon: Users, roles: ['admin', 'manager', 'auditor'] },
    { label: '토큰', path: '/access/tokens', icon: KeyRound },
    { label: '검토·승인', path: '/approvals', icon: Workflow, approval: true },
  ] },
  { label: '애플리케이션', items: [
    { label: '애플리케이션', path: '/applications', icon: AppWindow },
    { label: '환경', path: '/applications/environments', icon: Boxes },
    { label: '의존성 그래프', path: '/applications/dependencies', icon: GitBranch },
  ] },
  { label: '감사·관측', items: [
    { label: '감사 로그', path: '/audit', icon: FileSearch, roles: ['admin', 'manager', 'auditor'] },
    { label: '이벤트', path: '/audit/events', icon: Activity, roles: ['admin', 'manager', 'auditor'] },
    { label: '보안 이벤트', path: '/audit/security', icon: ShieldCheck, roles: ['admin', 'manager', 'auditor'] },
  ] },
  { label: '도구', items: [
    { label: 'AI 보안 도우미', path: '/ai', icon: Bot },
    { label: 'API 탐색기', path: '/api-explorer', icon: Cable },
  ] },
  { label: '인프라', items: [
    { label: '클러스터', path: '/infrastructure/cluster', icon: Network, admin: true },
    { label: '스토리지', path: '/infrastructure/storage', icon: Database, admin: true },
    { label: 'Seal·백업', path: '/infrastructure/security', icon: ServerCog, admin: true },
  ] },
  { label: '관리', items: [
    { label: '서비스 설정', path: '/admin/settings', icon: Settings, admin: true },
    { label: '사용자 관리', path: '/admin/users', icon: UserCog, admin: true },
    { label: 'Namespace', path: '/admin/namespaces', icon: BoxIcon, admin: true },
  ] },
];

interface PublicSettings { approval_enabled?: boolean; oidc_enabled?: boolean; version?: string }

function roleLabel(role?: string): string {
  return ({ admin: '서비스 관리자', manager: '팀장', auditor: '감사자', user: '일반 사용자' } as Record<string, string>)[role || ''] || '사용자';
}

function canViewNavItem(item: NavItem, role: string | undefined, approvalEnabled: boolean): boolean {
  if (item.admin && role !== 'admin') return false;
  if (item.roles && (!role || !item.roles.includes(role))) return false;
  if (item.approval && !approvalEnabled) return false;
  return true;
}

function SidebarContent({ close }: { close?: () => void }) {
  const { user } = useAuth();
  const settings = useQuery({ queryKey: ['public-settings'], queryFn: () => get<PublicSettings>('/settings/public'), retry: false });
  return (
    <>
      <Group h={78} px="lg" gap="sm">
        <Image src="/logo.png" alt="jikim 로고" w={42} h={42} fit="contain" />
        <Box>
          <Text size="xl" fw={850} className="brand-word" c="white">jikim</Text>
          <Text size="xs" c="#8fb3c1">Secrets Security</Text>
        </Box>
      </Group>
      <Divider color="rgba(255,255,255,.1)" />
      <nav className="sidebar-scroll" aria-label="주 메뉴">
        {navGroups.map((group) => {
          const items = group.items.filter((item) => canViewNavItem(item, user?.role, settings.data?.approval_enabled === true));
          if (!items.length) return null;
          return <Box key={group.label}><div className="nav-section">{group.label}</div>{items.map((item) => {
            const Icon = item.icon;
            return <NavLink key={item.path} to={item.path} onClick={close} className={({ isActive }) => `nav-link${isActive ? ' active' : ''}`}>
              <Icon size={18} strokeWidth={2} /><span>{item.label}</span>
            </NavLink>;
          })}</Box>;
        })}
      </nav>
      <Box px="md" py="sm">
        <Text size="xs" c="#89aaba">{settings.data?.version || APP_VERSION} · OpenBao API 제한 호환 프리뷰</Text>
      </Box>
    </>
  );
}

export function AppLayout() {
  const [mobileOpened, { open: openMobile, close: closeMobile }] = useDisclosure(false);
  const [collapsed, setCollapsed] = useState(false);
  const [searchOpened, { open: openSearch, close: closeSearch }] = useDisclosure(false);
  const [query, setQuery] = useState('');
  const { user, logout } = useAuth();
  const navigate = useNavigate();
  const location = useLocation();
  const publicSettings = useQuery({ queryKey: ['public-settings'], queryFn: () => get<PublicSettings>('/settings/public'), retry: false });

  useEffect(() => { closeMobile(); }, [location.pathname, closeMobile]);
  useEffect(() => {
    const handler = (event: KeyboardEvent) => {
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'k') { event.preventDefault(); openSearch(); }
    };
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, [openSearch]);

  const searchResults = useMemo(() => navGroups.flatMap((g) => g.items).filter((item) => (
    canViewNavItem(item, user?.role, publicSettings.data?.approval_enabled === true)
    && item.label.toLowerCase().includes(query.toLowerCase())
  )).slice(0, 9), [publicSettings.data?.approval_enabled, query, user?.role]);

  const doLogout = async () => {
    if (user?.auth_source === 'oidc') {
      window.location.assign('/api/v1/oidc/logout');
      return;
    }
    await logout();
    navigate('/login', { replace: true });
  };
  return (
    <AppShell
      className="app-shell"
      header={{ height: 68 }}
      navbar={{ width: collapsed ? 0 : 270, breakpoint: 'md', collapsed: { mobile: true, desktop: collapsed } }}
      padding={0}
    >
      <AppShell.Navbar className="sidebar"><SidebarContent /></AppShell.Navbar>
      <AppShell.Header className="main-header">
        <Group h="100%" px={{ base: 'md', sm: 'lg' }} justify="space-between" wrap="nowrap">
          <Group gap="sm" wrap="nowrap">
            <Burger hiddenFrom="md" opened={mobileOpened} onClick={openMobile} aria-label="메뉴 열기" />
            <Tooltip label={collapsed ? '메뉴 펼치기' : '메뉴 접기'} visibleFrom="md">
              <UnstyledButton visibleFrom="md" onClick={() => setCollapsed((value) => !value)} aria-label={collapsed ? '메뉴 펼치기' : '메뉴 접기'}>
                <ThemeIcon variant="subtle" color="gray" size="lg"><PanelLeftClose size={21} style={{ transform: collapsed ? 'rotate(180deg)' : undefined }} /></ThemeIcon>
              </UnstyledButton>
            </Tooltip>
            <Button variant="default" color="gray" leftSection={<Search size={18} />} rightSection={<span className="kbd">Ctrl K</span>} onClick={openSearch} w={{ base: 42, sm: 300 }} justify="flex-start">
              <span className="hide-mobile">메뉴 검색</span>
            </Button>
          </Group>
          <Group gap="sm" wrap="nowrap">
            <Tooltip label="도움말"><UnstyledButton aria-label="도움말" onClick={() => navigate('/guide')}><ThemeIcon variant="subtle" color="gray"><LifeBuoy size={20} /></ThemeIcon></UnstyledButton></Tooltip>
            <Menu position="bottom-end" width={285} shadow="xl" radius="md">
              <Menu.Target>
                <UnstyledButton aria-label="프로필 메뉴">
                  <Group gap="sm" wrap="nowrap">
                    <Avatar color="teal" radius="xl">{(user?.display_name || user?.username || '?').slice(0, 1).toUpperCase()}</Avatar>
                    <Box visibleFrom="sm"><Text size="sm" fw={700} lineClamp={1}>{user?.display_name || user?.username}</Text><Text size="xs" c="dimmed">{roleLabel(user?.role)}</Text></Box>
                    <ChevronDown size={16} className="hide-mobile" />
                  </Group>
                </UnstyledButton>
              </Menu.Target>
              <Menu.Dropdown className="profile-menu-scroll">
                <Box p="sm"><Text fw={750}>{user?.display_name || user?.username}</Text><Text size="sm" c="dimmed">{user?.email || '이메일 미등록'}</Text></Box>
                <Menu.Divider />
                <Menu.Item leftSection={<CircleUserRound size={18} />} onClick={() => navigate('/personal/profile')}>내 프로필</Menu.Item>
                <Menu.Item leftSection={<KeyRound size={18} />} onClick={() => navigate('/personal/keys')}>내 키·권한</Menu.Item>
                <Menu.Item leftSection={<SlidersHorizontal size={18} />} onClick={() => navigate('/personal/preferences')}>화면 설정</Menu.Item>
                {user?.role === 'admin' && <Menu.Item leftSection={<Settings size={18} />} onClick={() => navigate('/admin/settings')}>서비스 관리자 설정</Menu.Item>}
                <Menu.Divider />
                <Box px="sm" py={7}><Text size="xs" c="dimmed">jikim {APP_VERSION}</Text><Text size="xs" c="dimmed">OpenBao 2.6.1 API 제한 호환 프리뷰</Text></Box>
                <Menu.Divider />
                <Menu.Item color="red" leftSection={<LogOut size={18} />} onClick={() => void doLogout()}>로그아웃</Menu.Item>
              </Menu.Dropdown>
            </Menu>
          </Group>
        </Group>
      </AppShell.Header>
      <AppShell.Main><div className="content-wrap"><Outlet /></div></AppShell.Main>

      <Drawer opened={mobileOpened} onClose={closeMobile} withCloseButton={false} size={280} padding={0} classNames={{ content: 'sidebar' }}>
        <Group pos="absolute" right={10} top={18} style={{ zIndex: 2 }}><UnstyledButton onClick={closeMobile} aria-label="메뉴 닫기"><ThemeIcon color="gray" variant="transparent"><X color="white" /></ThemeIcon></UnstyledButton></Group>
        <SidebarContent close={closeMobile} />
      </Drawer>

      <Modal opened={searchOpened} onClose={closeSearch} title="메뉴 검색·빠른 이동" size="lg" overlayProps={{ className: 'command-overlay' }}>
        <TextInput autoFocus value={query} onChange={(event) => setQuery(event.currentTarget.value)} leftSection={<Search size={18} />} placeholder="메뉴 이름 검색" mb="md" />
        <ScrollArea.Autosize mah={440}>
          <Stack gap={4}>{searchResults.map((item) => { const Icon = item.icon; return (
            <UnstyledButton key={item.path} p="sm" style={{ borderRadius: 10 }} onClick={() => { navigate(item.path); closeSearch(); setQuery(''); }}>
              <Group><ThemeIcon variant="light" color="teal"><Icon size={18} /></ThemeIcon><Text fw={650}>{item.label}</Text></Group>
            </UnstyledButton>
          ); })}</Stack>
        </ScrollArea.Autosize>
      </Modal>
    </AppShell>
  );
}
