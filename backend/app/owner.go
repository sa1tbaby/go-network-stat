package app

import "time"

func (a *App) monitorOwner(ownerDone chan<- struct{}) {
	defer close(ownerDone)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			if a.cfg.OwnerPID > 0 && a.cfg.OwnerAlive != nil && !a.cfg.OwnerAlive(a.cfg.OwnerPID, a.cfg.OwnerStartTicks) {
				a.manager.Event("client_exit", "Client application exited")
				a.cancel()

				return
			}
		}
	}
}
