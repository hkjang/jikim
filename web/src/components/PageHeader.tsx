import { Breadcrumbs, Group, Stack, Text, Title } from '@mantine/core';
import { Link } from 'react-router-dom';

interface Crumb { label: string; href?: string }

export function PageHeader({ title, description, eyebrow, actions, crumbs }: {
  title: string; description?: string; eyebrow?: string; actions?: React.ReactNode; crumbs?: Crumb[];
}) {
  return (
    <Group justify="space-between" align="flex-end" mb="xl" gap="lg">
      <Stack gap={5}>
        {crumbs && <Breadcrumbs separator="/"><Link to="/">홈</Link>{crumbs.map((item) => item.href ? <Link key={item.label} to={item.href}>{item.label}</Link> : <Text key={item.label}>{item.label}</Text>)}</Breadcrumbs>}
        {eyebrow && <Text size="xs" fw={800} tt="uppercase" c="teal.8" lts=".08em">{eyebrow}</Text>}
        <Title order={1} fz={{ base: 27, sm: 32 }} className="page-title">{title}</Title>
        {description && <Text c="dimmed" maw={820} lh={1.65}>{description}</Text>}
      </Stack>
      {actions && <Group gap="sm">{actions}</Group>}
    </Group>
  );
}
