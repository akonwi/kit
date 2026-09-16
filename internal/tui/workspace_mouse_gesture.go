package tui

type workspaceMouseGestureController struct {
	generation   uint64
	lastReleased uint64
}

func (c *workspaceMouseGestureController) Press() {
	if c != nil {
		c.generation++
	}
}

func (c *workspaceMouseGestureController) Release() {
	if c != nil {
		c.lastReleased = c.generation
	}
}

func (c *workspaceMouseGestureController) Generation() uint64 {
	if c == nil {
		return 0
	}
	return c.generation
}

func (c *workspaceMouseGestureController) ReleasedGeneration() uint64 {
	if c == nil {
		return 0
	}
	return c.lastReleased
}
