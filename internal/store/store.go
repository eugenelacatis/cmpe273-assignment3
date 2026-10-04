package store

type Status int

const (
	Available Status = iota
	UnknownItem
	Insufficient
)

type Inventory struct {
	stock map[string]int
}

func New() *Inventory {
	return &Inventory{stock: map[string]int{"widget": 10, "gadget": 0, "gizmo": 3}}
}

func (i *Inventory) Check(itemID string, qty int) (Status, int) {
	have, ok := i.stock[itemID]
	if !ok {
		return UnknownItem, 0
	}
	if qty > have {
		return Insufficient, have
	}
	return Available, have
}
