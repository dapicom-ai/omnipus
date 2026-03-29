import { createFileRoute } from '@tanstack/react-router'
import { StatusBar } from '@/components/command-center/StatusBar'
import { TaskList } from '@/components/command-center/TaskList'

export function CommandCenterScreen() {
  return (
    <div className="flex flex-col h-full">
      <StatusBar />
      <TaskList />
    </div>
  )
}

export const Route = createFileRoute('/_app/command-center')({
  component: CommandCenterScreen,
})
