package logging

import "github.com/rs/zerolog"

// BaseLevel returns the configured level that ToggleDebug restores.
func (c *LevelController) BaseLevel() zerolog.Level {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.base
}

// CurrentLevel returns the currently active process logging level.
func (c *LevelController) CurrentLevel() zerolog.Level {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.current
}
