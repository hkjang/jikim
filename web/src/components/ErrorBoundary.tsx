import { Component, type ErrorInfo, type ReactNode } from 'react';
import { Alert, Button, Code, Group, Paper, Stack, Text } from '@mantine/core';
import { AlertCircle, RefreshCw, RotateCcw } from 'lucide-react';

interface ErrorBoundaryProps {
  children: ReactNode;
  /** Changing this value clears a captured error, e.g. on route change. */
  resetKey?: string;
}

interface ErrorBoundaryState {
  error: Error | null;
  resetKey?: string;
}

/**
 * Keeps a render-time exception from unmounting the whole application. Without
 * it React 19 tears down the entire tree and the user is left on a blank page
 * with no way back other than a manual reload.
 */
export class ErrorBoundary extends Component<ErrorBoundaryProps, ErrorBoundaryState> {
  state: ErrorBoundaryState = { error: null, resetKey: this.props.resetKey };

  static getDerivedStateFromError(error: Error): Partial<ErrorBoundaryState> {
    return { error };
  }

  static getDerivedStateFromProps(props: ErrorBoundaryProps, state: ErrorBoundaryState): Partial<ErrorBoundaryState> | null {
    if (props.resetKey !== state.resetKey) return { error: null, resetKey: props.resetKey };
    return null;
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error('화면 렌더링 중 오류가 발생했습니다.', error, info.componentStack);
  }

  render() {
    const { error } = this.state;
    if (!error) return this.props.children;

    return (
      <Paper className="surface" p="xl" radius="lg">
        <Stack align="flex-start">
          <Alert color="red" icon={<AlertCircle size={20} />} title="화면을 표시하지 못했습니다." radius="md" w="100%" role="alert">
            <Stack gap="sm">
              <Text>일시적인 오류로 이 화면을 그리지 못했습니다. 입력한 내용은 저장되지 않았을 수 있습니다.</Text>
              <Code block style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{error.message}</Code>
            </Stack>
          </Alert>
          <Group>
            <Button variant="light" leftSection={<RotateCcw size={17} />} onClick={() => this.setState({ error: null })}>
              다시 시도
            </Button>
            <Button variant="default" leftSection={<RefreshCw size={17} />} onClick={() => window.location.reload()}>
              새로고침
            </Button>
          </Group>
        </Stack>
      </Paper>
    );
  }
}
