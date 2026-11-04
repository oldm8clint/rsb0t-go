package actionsim

// A shop for the simulator: the keeper's stock and the player's pack side by side, Buy 1/5/10 on the stock and
// Sell 1/5/10 on the pack, no delay and no dialog (content shop/scripts/shop.rs2). Stackable stock only, one flat
// price per item, enough to prove that a shop's clicks are read five a tick.

import (
	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
)

type shopState struct {
	stock []Stack
	coins int32
	price int
}

// OpenShop opens the shop interface with stock (in slot order), paying and charging price coins an item in the
// coin object coinID.
func (s *Session) OpenShop(stock []Stack, coinID int32, price int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.shop = &shopState{stock: append([]Stack(nil), stock...), coins: coinID, price: price}
	s.srvBank = false
	s.feed(serverprot.IF_OPENMAIN, func(p *packet.Packet) { p.P2(actions.ShopRoot) })
	s.pushShop()
	s.pushPack()
}

// Stock is the shop's rows now.
func (s *Session) Stock() []Stack {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.shop == nil {
		return nil
	}
	return nonEmpty(s.shop.stock)
}

func (s *Session) pushShop() {
	if s.shop == nil {
		return
	}
	rows := append([]Stack(nil), s.shop.stock...)
	s.feed(serverprot.UPDATE_INV_FULL, func(p *packet.Packet) { writeInv(p, actions.ShopStock, rows) })
}

// shopButton applies an INV_BUTTON on the stock (buy) or the pack (sell) and reports whether the engine would
// count it as handled.
func (s *Session) shopButton(com, option int, obj int32, slot int) bool {
	qty := map[int]int{2: 1, 3: 5, 4: 10}[option]
	if qty == 0 {
		return true // Value
	}
	sh := s.shop
	switch com {
	case actions.ShopStock:
		if !at(sh.stock, slot, obj) {
			return false
		}
		if qty > sh.stock[slot].Count {
			qty = sh.stock[slot].Count
		}
		if affordable := s.packCount(sh.coins) / max(sh.price, 1); qty > affordable {
			qty = affordable
		}
		if qty > 0 {
			s.takePack(sh.coins, qty*sh.price)
			qty = s.addPack(obj, qty)
			sh.stock[slot].Count -= qty
		}
	case actions.ShopPack:
		if !at(s.pack, slot, obj) {
			return false
		}
		sold := s.takePack(obj, qty)
		if sold > 0 {
			s.addPack(sh.coins, sold*sh.price)
			shopAdd(sh, obj, sold)
		}
	}
	s.pushShop()
	s.pushPack()
	return true
}

func shopAdd(sh *shopState, id int32, n int) {
	for i := range sh.stock {
		if sh.stock[i].ID == id {
			sh.stock[i].Count += n
			return
		}
	}
	sh.stock = append(sh.stock, Stack{ID: id, Count: n})
}
