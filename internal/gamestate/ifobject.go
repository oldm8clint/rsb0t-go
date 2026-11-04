package gamestate

import "rsb0t-go/internal/packet"

// IF_SETOBJECT and IF_SETMODEL: which object a model component is currently showing.
//
// Make-X and smithing menus name their products only through this: the server sends
// IF_SETOBJECT (comId:u2, objId:u2, zoom:u2) for each product's picture, and Client.ts sets that
// component's model1Type to 4 (an object model) with model1Id = objId. ClientAdapter's
// makeProducts collects exactly those components. IF_SETMODEL (comId:u2, modelId:u2) switches a
// component back to a plain model (model1Type 1), so it clears the entry here.

// applyIfSetObject decodes IF_SETOBJECT. The zoom only affects rendering.
func (s *State) applyIfSetObject(r *packet.Packet) {
	comID := int(r.G2())
	objID := int(r.G2())
	_ = r.G2() // zoom
	if s.ifObjects == nil {
		s.ifObjects = make(map[int]int)
	}
	s.ifObjects[comID] = objID
}

// applyIfSetModel decodes IF_SETMODEL far enough to clear an object model from the component.
func (s *State) applyIfSetModel(r *packet.Packet) {
	delete(s.ifObjects, int(r.G2()))
}

// InterfaceObject returns the object id IF_SETOBJECT last put on component comID, porting
// ClientAdapter's ifModelObjId; ok is false when none is showing.
func (s *State) InterfaceObject(comID int) (objID int, ok bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	objID, ok = s.ifObjects[comID]
	return objID, ok
}
