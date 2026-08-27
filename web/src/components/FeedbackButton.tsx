import { ActionIcon, Button, Modal, Stack, Text, Textarea } from '@mantine/core';
import { useForm } from '@mantine/form';
import { useDisclosure } from '@mantine/hooks';
import { notifications } from '@mantine/notifications';
import { MessageSquareText } from 'lucide-react';
import { useLocation } from 'react-router';
import { useFeedback } from '../api/queries';

export function FeedbackButton() {
  const [opened, { open, close }] = useDisclosure(false);
  const location = useLocation();
  const feedback = useFeedback();
  const form = useForm({
    initialValues: { message: '' },
    validate: { message: (value) => value.trim().length >= 10 ? null : 'Please add at least 10 characters.' },
  });

  const submit = form.onSubmit(async ({ message }) => {
    try {
      await feedback.mutateAsync({ message: message.trim(), page_url: new URL(location.pathname, window.location.origin).toString() });
      notifications.show({ message: 'Thanks — your feedback was sent.', color: 'teal' });
      form.reset();
      close();
    } catch {
      notifications.show({ message: 'Feedback could not be sent. Please try again.', color: 'red' });
    }
  });

  return (
    <>
      <ActionIcon
        aria-label="Send feedback"
        className="feedback-trigger"
        color="dark"
        radius="xl"
        size="lg"
        onClick={open}
      >
        <MessageSquareText size={20} aria-hidden />
      </ActionIcon>
      <Modal opened={opened} onClose={close} title="Send feedback" centered>
        <form onSubmit={submit}>
          <Stack gap="sm">
            <Text size="sm" c="dimmed">Describe an issue or a missing context. Feedback is sent to the project owner.</Text>
            <Textarea label="Message" required minRows={5} autosize {...form.getInputProps('message')} />
            <Button type="submit" loading={feedback.isPending}>Send feedback</Button>
          </Stack>
        </form>
      </Modal>
    </>
  );
}
