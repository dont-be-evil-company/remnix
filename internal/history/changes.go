package history

type CompletionChange struct {
	ID   string
	End  int64
	Exit int
}

type ChangeSet struct {
	Created    []Entry
	Completed  []CompletionChange
	Tombstoned []string
	Commands   []string
}

func (c ChangeSet) Empty() bool {
	return len(c.Created) == 0 && len(c.Completed) == 0 && len(c.Tombstoned) == 0 && len(c.Commands) == 0
}

func (c *ChangeSet) AddCreated(e Entry) {
	c.Created = append(c.Created, e)
}

func (c *ChangeSet) AddCompleted(id string, end int64, exit int) {
	c.Completed = append(c.Completed, CompletionChange{ID: id, End: end, Exit: exit})
}

func (c *ChangeSet) AddTombstoned(ids ...string) {
	c.Tombstoned = append(c.Tombstoned, ids...)
}

func (c *ChangeSet) Merge(other ChangeSet) {
	c.Created = append(c.Created, other.Created...)
	c.Completed = append(c.Completed, other.Completed...)
	c.Tombstoned = append(c.Tombstoned, other.Tombstoned...)
	c.Commands = append(c.Commands, other.Commands...)
}
