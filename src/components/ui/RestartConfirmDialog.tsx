import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter, DialogDescription } from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'

interface RestartConfirmDialogProps {
  open: boolean
  onConfirm: () => void
  onCancel: () => void
  saving?: boolean
}

export function RestartConfirmDialog({ open, onConfirm, onCancel, saving }: RestartConfirmDialogProps) {
  return (
    <Dialog open={open} onOpenChange={(v) => { if (!v) onCancel() }}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Restart Required</DialogTitle>
          <DialogDescription>
            This change requires an Omnipus gateway restart to take effect. The setting will be saved immediately, but won't apply until the gateway is restarted.
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="outline" onClick={onCancel} disabled={saving}>Cancel</Button>
          <Button onClick={onConfirm} disabled={saving}>
            {saving ? 'Saving...' : 'Save & Restart Later'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
