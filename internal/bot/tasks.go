package bot

import (
	"fmt"
	"time"
)

// TaskBot and TreeBot port Bot.ts's two dispatch patterns on top of the Bot interface, so a
// script can be written as prioritised tasks or a behaviour tree instead of one hand-rolled
// Loop:
//
//   - TaskBot runs the first Task whose Validate passes, once per loop; order is priority.
//   - TreeBot walks BranchTasks from Root until it reaches a LeafTask, then runs it.
//
// Both refuse to dispatch until the scene is ready (Context.SceneReady), logging at most once
// every sceneWaitLogInterval while they wait. Upstream added that gate because a mid-zone
// rebuild leaves the client in game while the scene is still building, and tasks that still
// validate then send input the server silently drops. Both also pace every loop with their
// Cadence, including the waiting and nothing-validated cases, so a Runner calling Loop back to
// back can never spin.

// sceneWaitLogInterval throttles the "waiting for scene" log line, as Bot.ts does.
const sceneWaitLogInterval = 2 * time.Second

// maxTreeDepth bounds how many branches one TreeBot loop may walk, so a tree whose branches
// point back at each other fails with an error instead of hanging the bot.
const maxTreeDepth = 256

// Task is a guard and the action it guards.
type Task interface {
	Validate(ctx *Context) bool
	Execute(ctx *Context) error
}

// TaskFunc adapts two functions into a Task.
type TaskFunc struct {
	ValidateFunc func(ctx *Context) bool
	ExecuteFunc  func(ctx *Context) error
}

// Validate calls ValidateFunc; a nil ValidateFunc never validates.
func (t TaskFunc) Validate(ctx *Context) bool {
	return t.ValidateFunc != nil && t.ValidateFunc(ctx)
}

// Execute calls ExecuteFunc; a nil ExecuteFunc does nothing.
func (t TaskFunc) Execute(ctx *Context) error {
	if t.ExecuteFunc == nil {
		return nil
	}
	return t.ExecuteFunc(ctx)
}

// TaskBot is a Bot that runs the first valid Task each loop.
type TaskBot struct {
	Tasks []Task
	// Cadence paces each loop; the zero value is one server tick.
	Cadence Cadence
	// Start and Stop, when set, are the bot's OnStart and OnStop.
	Start func(ctx *Context) error
	Stop  func(ctx *Context)

	lastSceneLog time.Time
}

// Add appends tasks at the lowest priority.
func (b *TaskBot) Add(tasks ...Task) {
	b.Tasks = append(b.Tasks, tasks...)
}

// OnStart runs Start, if set.
func (b *TaskBot) OnStart(ctx *Context) error {
	if b.Start != nil {
		return b.Start(ctx)
	}
	return nil
}

// OnStop runs Stop, if set.
func (b *TaskBot) OnStop(ctx *Context) {
	if b.Stop != nil {
		b.Stop(ctx)
	}
}

// Loop waits for a ready scene, runs the first Task whose Validate passes, then paces.
func (b *TaskBot) Loop(ctx *Context) error {
	if !ctx.SceneReady() {
		logSceneWait(ctx, &b.lastSceneLog, "tasks")
		ctx.Pace(b.Cadence)
		return nil
	}
	for _, task := range b.Tasks {
		if ctx.Stopped() {
			return nil
		}
		if task.Validate(ctx) {
			if err := task.Execute(ctx); err != nil {
				return err
			}
			break
		}
	}
	ctx.Pace(b.Cadence)
	return nil
}

// TreeNode is a BranchTask or a LeafTask.
type TreeNode interface{}

// BranchTask is a decision node: Validate picks Success or Failure as the next node.
type BranchTask interface {
	Validate(ctx *Context) bool
	Success() TreeNode
	Failure() TreeNode
}

// LeafTask is an action node.
type LeafTask interface {
	Execute(ctx *Context) error
}

// TreeBot is a Bot that walks a behaviour tree from Root each loop.
type TreeBot struct {
	// Root returns the tree's root node; it is called once per loop.
	Root func(ctx *Context) TreeNode
	// Cadence paces each loop; the zero value is one server tick.
	Cadence Cadence
	// Start and Stop, when set, are the bot's OnStart and OnStop.
	Start func(ctx *Context) error
	Stop  func(ctx *Context)

	lastSceneLog time.Time
}

// OnStart runs Start, if set, and rejects a TreeBot without a Root.
func (b *TreeBot) OnStart(ctx *Context) error {
	if b.Root == nil {
		return fmt.Errorf("bot: TreeBot has no Root")
	}
	if b.Start != nil {
		return b.Start(ctx)
	}
	return nil
}

// OnStop runs Stop, if set.
func (b *TreeBot) OnStop(ctx *Context) {
	if b.Stop != nil {
		b.Stop(ctx)
	}
}

// Loop waits for a ready scene, follows branches from Root to a leaf, runs it, then paces. A
// node that is both a BranchTask and a LeafTask is treated as a branch, as Bot.ts's instanceof
// check does.
func (b *TreeBot) Loop(ctx *Context) error {
	if !ctx.SceneReady() {
		logSceneWait(ctx, &b.lastSceneLog, "tree")
		ctx.Pace(b.Cadence)
		return nil
	}
	node := b.Root(ctx)
	for depth := 0; ; depth++ {
		switch n := node.(type) {
		case BranchTask:
			if depth >= maxTreeDepth {
				return fmt.Errorf("bot: behaviour tree deeper than %d branches", maxTreeDepth)
			}
			if n.Validate(ctx) {
				node = n.Success()
			} else {
				node = n.Failure()
			}
		case LeafTask:
			if err := n.Execute(ctx); err != nil {
				return err
			}
			ctx.Pace(b.Cadence)
			return nil
		default:
			return fmt.Errorf("bot: behaviour tree node %T is neither a BranchTask nor a LeafTask", node)
		}
	}
}

func logSceneWait(ctx *Context, last *time.Time, what string) {
	if time.Since(*last) < sceneWaitLogInterval {
		return
	}
	*last = time.Now()
	state := -1
	if st := ctx.gameState(); st != nil {
		state = st.SceneState()
	}
	ctx.logf("waiting for scene (state=%d) before %s", state, what)
}
