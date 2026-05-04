import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import {
  DotsThreeVertical,
  UserCircle,
  Circle,
  Warning,
} from '@phosphor-icons/react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Badge } from '@/components/ui/badge'
import {
  Table,
  TableHeader,
  TableBody,
  TableRow,
  TableHead,
  TableCell,
} from '@/components/ui/table'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
  DialogDescription,
} from '@/components/ui/dialog'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
  DropdownMenuSeparator,
} from '@/components/ui/dropdown-menu'
import { useUiStore } from '@/store/ui'
import { useAuthStore } from '@/store/auth'
import {
  fetchUsers,
  createUser,
  deleteUser,
  resetUserPassword,
  updateUserRole,
  changePassword,
  isApiError,
} from '@/lib/api'
import type { UserEntry, UserRole } from '@/lib/api'

// ── Username validation regex ─────────────────────────────────────────────────
const USERNAME_RE = /^[A-Za-z0-9][A-Za-z0-9._-]{1,62}$/

// ── Add User Dialog ───────────────────────────────────────────────────────────

interface AddUserDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onCreated: () => void
}

function AddUserDialog({ open, onOpenChange, onCreated }: AddUserDialogProps) {
  const { addToast } = useUiStore()
  const [username, setUsername] = useState('')
  const [role, setRole] = useState<UserRole>('user')
  const [password, setPassword] = useState('')
  const [usernameError, setUsernameError] = useState<string | null>(null)
  const [passwordError, setPasswordError] = useState<string | null>(null)
  const [serverError, setServerError] = useState<string | null>(null)

  const { mutate: submit, isPending } = useMutation({
    mutationFn: () => createUser({ username, role, password }),
    onSuccess: (resp) => {
      addToast({
        message: 'User created. They can now log in with the password you set.',
        variant: 'success',
      })
      if (resp.warning) {
        addToast({ variant: 'warning', message: resp.warning })
      }
      resetForm()
      onOpenChange(false)
      onCreated()
    },
    onError: (err: unknown) => {
      setServerError(isApiError(err) ? err.userMessage : err instanceof Error ? err.message : 'Failed to create user')
    },
  })

  function resetForm() {
    setUsername('')
    setRole('user')
    setPassword('')
    setUsernameError(null)
    setPasswordError(null)
    setServerError(null)
  }

  function handleOpenChange(value: boolean) {
    if (!value) resetForm()
    onOpenChange(value)
  }

  function validate(): boolean {
    let valid = true
    setUsernameError(null)
    setPasswordError(null)
    setServerError(null)
    if (!USERNAME_RE.test(username)) {
      setUsernameError(
        'Use only letters, numbers, `.`, `_`, `-` (no spaces, no slashes).',
      )
      valid = false
    }
    if (password.length < 8) {
      setPasswordError('Password must be at least 8 characters.')
      valid = false
    }
    return valid
  }

  function handleSubmit() {
    if (validate()) {
      submit()
    }
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Add user</DialogTitle>
          <DialogDescription>
            The new user can log in with the password you set here.
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-4 py-2">
          <div className="space-y-1.5">
            <Label htmlFor="add-username" className="text-xs text-[var(--color-secondary)]">
              Username
            </Label>
            <Input
              id="add-username"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              placeholder="alice"
              className="h-8 text-xs"
              autoComplete="off"
            />
            {usernameError && (
              <p className="text-xs text-[var(--color-error)]">{usernameError}</p>
            )}
          </div>

          <div className="space-y-1.5">
            <Label className="text-xs text-[var(--color-secondary)]">Role</Label>
            <div className="flex gap-4">
              <label className="flex items-center gap-2 text-sm text-[var(--color-secondary)] cursor-pointer">
                <input
                  type="radio"
                  name="add-role"
                  value="user"
                  checked={role === 'user'}
                  onChange={() => setRole('user')}
                  className="accent-[var(--color-accent)]"
                />
                User
              </label>
              <label className="flex items-center gap-2 text-sm text-[var(--color-secondary)] cursor-pointer">
                <input
                  type="radio"
                  name="add-role"
                  value="admin"
                  checked={role === 'admin'}
                  onChange={() => setRole('admin')}
                  className="accent-[var(--color-accent)]"
                />
                Admin
              </label>
            </div>
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="add-password" className="text-xs text-[var(--color-secondary)]">
              Password
            </Label>
            <Input
              id="add-password"
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder="Min 8 characters"
              className="h-8 text-xs"
              autoComplete="new-password"
            />
            {passwordError && (
              <p className="text-xs text-[var(--color-error)]">{passwordError}</p>
            )}
          </div>

          {serverError && (
            <p className="text-xs text-[var(--color-error)]">{serverError}</p>
          )}
        </div>
        <DialogFooter>
          <Button variant="outline" size="sm" onClick={() => handleOpenChange(false)} disabled={isPending}>
            Cancel
          </Button>
          <Button size="sm" onClick={handleSubmit} disabled={isPending}>
            {isPending ? 'Creating...' : 'Create user'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// ── Change Role Dialog ────────────────────────────────────────────────────────

interface ChangeRoleDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  target: UserEntry | null
  onChanged: () => void
}

function ChangeRoleDialog({ open, onOpenChange, target, onChanged }: ChangeRoleDialogProps) {
  const { addToast } = useUiStore()
  const [role, setRole] = useState<UserRole>(target?.role ?? 'user')
  const [serverError, setServerError] = useState<string | null>(null)

  const { mutate: submit, isPending } = useMutation({
    mutationFn: () => updateUserRole(target!.username, role),
    onSuccess: (resp) => {
      addToast({ message: `Role updated for ${target!.username}.`, variant: 'success' })
      if (resp.warning) {
        addToast({ variant: 'warning', message: resp.warning })
      }
      setServerError(null)
      onOpenChange(false)
      onChanged()
    },
    onError: (err: unknown) => {
      setServerError(isApiError(err) ? err.userMessage : err instanceof Error ? err.message : 'Failed to update role')
    },
  })

  function handleOpenChange(value: boolean) {
    if (!value) setServerError(null)
    if (!value && target) setRole(target.role)
    onOpenChange(value)
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className="sm:max-w-sm">
        <DialogHeader>
          <DialogTitle>Change role</DialogTitle>
          <DialogDescription>
            Update the role for <strong>{target?.username}</strong>.
          </DialogDescription>
        </DialogHeader>
        <div className="py-2">
          <div className="flex gap-4">
            <label className="flex items-center gap-2 text-sm text-[var(--color-secondary)] cursor-pointer">
              <input
                type="radio"
                name="change-role"
                value="user"
                checked={role === 'user'}
                onChange={() => setRole('user')}
                className="accent-[var(--color-accent)]"
              />
              User
            </label>
            <label className="flex items-center gap-2 text-sm text-[var(--color-secondary)] cursor-pointer">
              <input
                type="radio"
                name="change-role"
                value="admin"
                checked={role === 'admin'}
                onChange={() => setRole('admin')}
                className="accent-[var(--color-accent)]"
              />
              Admin
            </label>
          </div>
          {serverError && (
            <p className="text-xs text-[var(--color-error)] mt-2">{serverError}</p>
          )}
        </div>
        <DialogFooter>
          <Button variant="outline" size="sm" onClick={() => handleOpenChange(false)} disabled={isPending}>
            Cancel
          </Button>
          <Button size="sm" onClick={() => submit()} disabled={isPending}>
            {isPending ? 'Saving...' : 'Save'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// ── Reset Password Dialog (for other users) ───────────────────────────────────

interface ResetPasswordDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  target: UserEntry | null
  onReset: () => void
}

function ResetPasswordDialog({ open, onOpenChange, target, onReset }: ResetPasswordDialogProps) {
  const { addToast } = useUiStore()
  const [newPassword, setNewPassword] = useState('')
  const [passwordError, setPasswordError] = useState<string | null>(null)
  const [serverError, setServerError] = useState<string | null>(null)

  const { mutate: submit, isPending } = useMutation({
    mutationFn: () => resetUserPassword(target!.username, newPassword),
    onSuccess: (resp) => {
      addToast({
        message: `Password reset for ${target!.username}. Their old bearer token is now invalid.`,
        variant: 'success',
      })
      if (resp.warning) {
        addToast({ variant: 'warning', message: resp.warning })
      }
      setNewPassword('')
      setPasswordError(null)
      setServerError(null)
      onOpenChange(false)
      onReset()
    },
    onError: (err: unknown) => {
      setServerError(isApiError(err) ? err.userMessage : err instanceof Error ? err.message : 'Failed to reset password')
    },
  })

  function handleOpenChange(value: boolean) {
    if (!value) {
      setNewPassword('')
      setPasswordError(null)
      setServerError(null)
    }
    onOpenChange(value)
  }

  function handleSubmit() {
    setPasswordError(null)
    setServerError(null)
    if (newPassword.length < 8) {
      setPasswordError('Password must be at least 8 characters.')
      return
    }
    submit()
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className="sm:max-w-sm">
        <DialogHeader>
          <DialogTitle>Reset password</DialogTitle>
          <DialogDescription>
            Set a new password for <strong>{target?.username}</strong>. Their old bearer
            token will be invalidated immediately.
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-3 py-2">
          <div className="space-y-1.5">
            <Label htmlFor="reset-password" className="text-xs text-[var(--color-secondary)]">
              New password
            </Label>
            <Input
              id="reset-password"
              type="password"
              value={newPassword}
              onChange={(e) => setNewPassword(e.target.value)}
              placeholder="Min 8 characters"
              className="h-8 text-xs"
              autoComplete="new-password"
            />
            {passwordError && (
              <p className="text-xs text-[var(--color-error)]">{passwordError}</p>
            )}
          </div>
          {serverError && (
            <p className="text-xs text-[var(--color-error)]">{serverError}</p>
          )}
        </div>
        <DialogFooter>
          <Button variant="outline" size="sm" onClick={() => handleOpenChange(false)} disabled={isPending}>
            Cancel
          </Button>
          <Button size="sm" onClick={handleSubmit} disabled={isPending}>
            {isPending ? 'Saving...' : 'Reset password'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// ── Change My Password Dialog (own row) ───────────────────────────────────────

interface ChangeMyPasswordDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

function ChangeMyPasswordDialog({ open, onOpenChange }: ChangeMyPasswordDialogProps) {
  const { addToast } = useUiStore()
  const [currentPassword, setCurrentPassword] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [currentError, setCurrentError] = useState<string | null>(null)
  const [newError, setNewError] = useState<string | null>(null)
  const [serverError, setServerError] = useState<string | null>(null)

  const { mutate: submit, isPending } = useMutation({
    mutationFn: () => changePassword(currentPassword, newPassword),
    onSuccess: () => {
      addToast({ message: 'Password changed successfully.', variant: 'success' })
      resetForm()
      onOpenChange(false)
    },
    onError: (err: unknown) => {
      setServerError(isApiError(err) ? err.userMessage : err instanceof Error ? err.message : 'Failed to change password')
    },
  })

  function resetForm() {
    setCurrentPassword('')
    setNewPassword('')
    setCurrentError(null)
    setNewError(null)
    setServerError(null)
  }

  function handleOpenChange(value: boolean) {
    if (!value) resetForm()
    onOpenChange(value)
  }

  function handleSubmit() {
    setCurrentError(null)
    setNewError(null)
    setServerError(null)
    let valid = true
    if (!currentPassword) {
      setCurrentError('Current password is required.')
      valid = false
    }
    if (newPassword.length < 8) {
      setNewError('New password must be at least 8 characters.')
      valid = false
    }
    if (valid) submit()
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className="sm:max-w-sm">
        <DialogHeader>
          <DialogTitle>Change my password</DialogTitle>
          <DialogDescription>
            Update your own login password. You must provide your current password.
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-3 py-2">
          <div className="space-y-1.5">
            <Label htmlFor="my-current-password" className="text-xs text-[var(--color-secondary)]">
              Current password
            </Label>
            <Input
              id="my-current-password"
              type="password"
              value={currentPassword}
              onChange={(e) => setCurrentPassword(e.target.value)}
              className="h-8 text-xs"
              autoComplete="current-password"
            />
            {currentError && (
              <p className="text-xs text-[var(--color-error)]">{currentError}</p>
            )}
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="my-new-password" className="text-xs text-[var(--color-secondary)]">
              New password
            </Label>
            <Input
              id="my-new-password"
              type="password"
              value={newPassword}
              onChange={(e) => setNewPassword(e.target.value)}
              placeholder="Min 8 characters"
              className="h-8 text-xs"
              autoComplete="new-password"
            />
            {newError && (
              <p className="text-xs text-[var(--color-error)]">{newError}</p>
            )}
          </div>
          {serverError && (
            <p className="text-xs text-[var(--color-error)]">{serverError}</p>
          )}
        </div>
        <DialogFooter>
          <Button variant="outline" size="sm" onClick={() => handleOpenChange(false)} disabled={isPending}>
            Cancel
          </Button>
          <Button size="sm" onClick={handleSubmit} disabled={isPending}>
            {isPending ? 'Saving...' : 'Change password'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// ── Delete User Dialog ────────────────────────────────────────────────────────

interface DeleteUserDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  target: UserEntry | null
  onDeleted: () => void
}

function DeleteUserDialog({ open, onOpenChange, target, onDeleted }: DeleteUserDialogProps) {
  const { addToast } = useUiStore()
  const [inlineError, setInlineError] = useState<string | null>(null)

  const { mutate: submit, isPending } = useMutation({
    mutationFn: () => deleteUser(target!.username),
    onSuccess: (resp) => {
      addToast({ message: `User ${target!.username} deleted.`, variant: 'success' })
      if (resp.warning) {
        addToast({ variant: 'warning', message: resp.warning })
      }
      setInlineError(null)
      onOpenChange(false)
      onDeleted()
    },
    onError: (err: unknown) => {
      // 409 last-admin error: show inline, keep dialog open. Defensively
      // also match the legacy "administrator" substring for any server
      // version that returns the message with a different status code.
      if (isApiError(err)) {
        if (err.status === 409 || err.userMessage.toLowerCase().includes('administrator')) {
          setInlineError('Cannot leave the deployment with zero administrators.')
        } else {
          setInlineError(err.userMessage)
        }
      } else {
        setInlineError(err instanceof Error ? err.message : 'Failed to delete user')
      }
    },
  })

  function handleOpenChange(value: boolean) {
    if (!value) setInlineError(null)
    onOpenChange(value)
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className="sm:max-w-sm">
        <DialogHeader>
          <DialogTitle>Delete user</DialogTitle>
          <DialogDescription>
            Delete user <strong>{target?.username}</strong>? This action cannot be undone.
          </DialogDescription>
        </DialogHeader>
        {inlineError && (
          <div className="flex items-start gap-2 rounded-md border border-[var(--color-error)]/40 bg-[var(--color-error)]/10 px-3 py-2">
            <Warning size={16} className="text-[var(--color-error)] shrink-0 mt-0.5" />
            <p className="text-xs text-[var(--color-error)]">{inlineError}</p>
          </div>
        )}
        <DialogFooter>
          <Button variant="outline" size="sm" onClick={() => handleOpenChange(false)} disabled={isPending}>
            Cancel
          </Button>
          <Button
            size="sm"
            variant="destructive"
            onClick={() => { setInlineError(null); submit() }}
            disabled={isPending}
          >
            {isPending ? 'Deleting...' : 'Delete'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// ── Row Action Menu ───────────────────────────────────────────────────────────

interface RowActionsProps {
  user: UserEntry
  isOwnRow: boolean
  isOnlyAdmin: boolean
  onChangeRole: () => void
  onResetPassword: () => void
  onChangeMyPassword: () => void
  onDelete: () => void
}

function RowActions({
  user,
  isOwnRow,
  isOnlyAdmin,
  onChangeRole,
  onResetPassword,
  onChangeMyPassword,
  onDelete,
}: RowActionsProps) {
  const deleteDisabled = isOwnRow && isOnlyAdmin

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          variant="ghost"
          size="sm"
          className="h-7 w-7 p-0"
          aria-label={`Actions for ${user.username}`}
        >
          <DotsThreeVertical size={16} />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="min-w-[180px]">
        <DropdownMenuItem onClick={onChangeRole}>
          Change role
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        {isOwnRow ? (
          <DropdownMenuItem onClick={onChangeMyPassword}>
            Change my password
          </DropdownMenuItem>
        ) : (
          <DropdownMenuItem onClick={onResetPassword}>
            Reset password
          </DropdownMenuItem>
        )}
        <DropdownMenuSeparator />
        <DropdownMenuItem
          onClick={deleteDisabled ? undefined : onDelete}
          disabled={deleteDisabled}
          className="text-[var(--color-error)] focus:text-[var(--color-error)]"
          title={
            deleteDisabled
              ? 'You can delete your own account only if another admin exists.'
              : undefined
          }
        >
          Delete
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

// ── Main Section ──────────────────────────────────────────────────────────────

interface UsersSectionProps {
  devModeBypass?: boolean
}

export function UsersSection({ devModeBypass = false }: UsersSectionProps) {
  const { addToast } = useUiStore()
  const currentUsername = useAuthStore((s) => s.username)
  const queryClient = useQueryClient()

  // Dialog state
  const [addDialogOpen, setAddDialogOpen] = useState(false)
  const [changeRoleTarget, setChangeRoleTarget] = useState<UserEntry | null>(null)
  const [resetPasswordTarget, setResetPasswordTarget] = useState<UserEntry | null>(null)
  const [changeMyPasswordOpen, setChangeMyPasswordOpen] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState<UserEntry | null>(null)

  const {
    data: users,
    isLoading,
    isError,
    error,
    refetch,
  } = useQuery({
    queryKey: ['users'],
    queryFn: fetchUsers,
    enabled: !devModeBypass,
  })

  function refreshUsers() {
    queryClient.invalidateQueries({ queryKey: ['users'] })
  }

  if (devModeBypass) {
    return (
      <div className="rounded-lg border border-[var(--color-warning)]/40 bg-[var(--color-warning)]/10 px-4 py-3 flex items-start gap-2">
        <Warning size={16} className="text-[var(--color-warning)] shrink-0 mt-0.5" />
        <p className="text-sm text-[var(--color-warning)]">
          User management is disabled in dev-mode-bypass. Restart the gateway with{' '}
          <code className="font-mono text-xs">dev_mode_bypass=false</code> to use these controls.
        </p>
      </div>
    )
  }

  const adminCount = users?.filter((u) => u.role === 'admin').length ?? 0

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="font-headline font-bold text-base text-[var(--color-secondary)]">
            Users
          </h2>
          <p className="text-xs text-[var(--color-muted)] mt-0.5">
            Manage user accounts and roles.
          </p>
        </div>
        <Button
          size="sm"
          onClick={() => setAddDialogOpen(true)}
          className="gap-1.5"
        >
          <UserCircle size={15} weight="bold" />
          Add user
        </Button>
      </div>

      {isLoading && (
        <div className="rounded-lg border border-[var(--color-border)] bg-[var(--color-surface-1)] p-8 flex items-center justify-center">
          <p className="text-sm text-[var(--color-muted)]">Loading users...</p>
        </div>
      )}

      {isError && (
        <div className="rounded-lg border border-[var(--color-error)]/40 bg-[var(--color-error)]/10 px-4 py-3 flex items-center justify-between gap-4">
          <p className="text-sm text-[var(--color-error)]">
            {(error as Error)?.message ?? 'Failed to load users.'}
          </p>
          <Button
            size="sm"
            variant="outline"
            onClick={() => {
              refetch().catch((e: unknown) => {
                addToast({ message: String(e), variant: 'error' })
              })
            }}
          >
            Retry
          </Button>
        </div>
      )}

      {!isLoading && !isError && users && (
        <div className="rounded-lg border border-[var(--color-border)] overflow-hidden">
          <Table aria-label="User accounts">
            <TableHeader>
              <TableRow>
                <TableHead>Username</TableHead>
                <TableHead>Role</TableHead>
                <TableHead>Has active token</TableHead>
                <TableHead className="w-12" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {users.length === 0 && (
                <TableRow>
                  <TableCell colSpan={4} className="text-center text-sm text-[var(--color-muted)] py-6">
                    No users found.
                  </TableCell>
                </TableRow>
              )}
              {users.map((user) => {
                const isOwnRow = user.username === currentUsername
                const isOnlyAdmin = user.role === 'admin' && adminCount === 1

                return (
                  <TableRow key={user.username}>
                    <TableCell className="font-mono text-xs text-[var(--color-secondary)]">
                      {user.username}
                      {isOwnRow && (
                        <span className="ml-2 text-[10px] text-[var(--color-muted)]">(you)</span>
                      )}
                    </TableCell>
                    <TableCell>
                      <Badge variant={user.role === 'admin' ? 'default' : 'secondary'}>
                        {user.role}
                      </Badge>
                    </TableCell>
                    <TableCell>
                      {user.has_active_token ? (
                        <span
                          className="flex items-center gap-1.5 text-xs text-[var(--color-success)]"
                          title="User has logged in at least once"
                        >
                          <Circle size={8} weight="fill" className="text-[var(--color-success)]" />
                          Active
                        </span>
                      ) : (
                        <span className="text-xs text-[var(--color-muted)]">—</span>
                      )}
                    </TableCell>
                    <TableCell>
                      <RowActions
                        user={user}
                        isOwnRow={isOwnRow}
                        isOnlyAdmin={isOnlyAdmin}
                        onChangeRole={() => setChangeRoleTarget(user)}
                        onResetPassword={() => setResetPasswordTarget(user)}
                        onChangeMyPassword={() => setChangeMyPasswordOpen(true)}
                        onDelete={() => setDeleteTarget(user)}
                      />
                    </TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
        </div>
      )}

      <AddUserDialog
        open={addDialogOpen}
        onOpenChange={setAddDialogOpen}
        onCreated={refreshUsers}
      />

      <ChangeRoleDialog
        open={changeRoleTarget !== null}
        onOpenChange={(v) => { if (!v) setChangeRoleTarget(null) }}
        target={changeRoleTarget}
        onChanged={refreshUsers}
      />

      <ResetPasswordDialog
        open={resetPasswordTarget !== null}
        onOpenChange={(v) => { if (!v) setResetPasswordTarget(null) }}
        target={resetPasswordTarget}
        onReset={refreshUsers}
      />

      <ChangeMyPasswordDialog
        open={changeMyPasswordOpen}
        onOpenChange={setChangeMyPasswordOpen}
      />

      <DeleteUserDialog
        open={deleteTarget !== null}
        onOpenChange={(v) => { if (!v) setDeleteTarget(null) }}
        target={deleteTarget}
        onDeleted={refreshUsers}
      />
    </div>
  )
}
