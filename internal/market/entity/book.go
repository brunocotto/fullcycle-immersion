package entity

import (
	"container/heap"
	"sync"
)

type Book struct {
	Order        []*Order
	Transactions []*Transaction
	OrdersChan   chan *Order // input => orders kafka
	OrderChanOut chan *Order
	Wg           *sync.WaitGroup
}

func NewBook(orderChan chan *Order, orderChanOut chan *Order, wg *sync.WaitGroup) *Book {
	return &Book{
		Order:        []*Order{},
		Transactions: []*Transaction{},
		OrdersChan:   orderChan,
		OrderChanOut: orderChanOut,
		Wg:           wg,
	}
}

// método de negociação
func (b *Book) Trade() {
	buyOrders := NewOrderQueue()
	sellOrders := NewOrderQueue()

	heap.Init(buyOrders)
	heap.Init(sellOrders)

	for order := range b.OrdersChan {
		// a ordem é de compra?
		if order.OrderType == "BUY" {
			buyOrders.Push(order)
			// existe alguma ordem de venda?
			// se existe, tem alguma ordem de venda com um preço menor ou igual a ordem de compra?
			if sellOrders.Len() > 0 && sellOrders.Orders[0].Price <= order.Price {
				sellOrder := sellOrders.Pop().(*Order)
				if sellOrder.PendingShares > 0 {
					transaction := NewTransaction(sellOrder, order, order.Shares, sellOrder.Price)
					b.AddTransaction(transaction, b.Wg)
					sellOrder.Transactions = append(sellOrder.Transactions, transaction)
					b.OrderChanOut <- sellOrder
					b.OrderChanOut <- order
					// se ainda falta cotas, retornamos uma nova transação para a fila com o valor que faltou ser negociado
					if sellOrder.PendingShares > 0 {
						sellOrders.Push(sellOrder)
					}
				}
			}
			// a ordem é de venda?
		} else if order.OrderType == "SELL" {
			sellOrders.Push(order)
			if buyOrders.Len() > 0 && buyOrders.Orders[0].Price >= order.Price {
				buyOrder := buyOrders.Pop().(*Order)
				if buyOrder.PendingShares > 0 {
					transaction := NewTransaction(order, buyOrder, order.Shares, buyOrder.Price)
					b.AddTransaction(transaction, b.Wg)
					buyOrder.Transactions = append(buyOrder.Transactions, transaction)
					order.Transactions = append(order.Transactions, transaction)
					b.OrderChanOut <- buyOrder
					b.OrderChanOut <- order
					if buyOrder.PendingShares > 0 {
						buyOrders.Push(buyOrder)
					}
				}
			}
		}
	}
}

func (b *Book) AddTransaction(transaction *Transaction, wg *sync.WaitGroup) {
	defer wg.Done()

	sellingShares := transaction.SellingOrder.PendingShares
	buyingShares := transaction.BuyingOrder.PendingShares

	minShares := sellingShares
	if buyingShares < minShares {
		minShares = buyingShares
	}
	//ordem de venda => o cara que vende => transação com um valor menor do que a venda desejada
	transaction.SellingOrder.Investor.UpdateAssetPosition(transaction.SellingOrder.Asset.ID, -minShares)
	transaction.AddSellOrderPendingShares(-minShares)

	//ordem de compra => o cara que compra => transação com um valor menor do que a compra desejada
	transaction.BuyingOrder.Investor.UpdateAssetPosition(transaction.BuyingOrder.Investor.ID, +minShares)
	transaction.AddBuyOrderPendingShares(-minShares)

	transaction.CalculateTotal(transaction.Shares, transaction.BuyingOrder.Price)

	//métodos implementados para verifidas se PendingShares == 0
	transaction.CloseBuyOrder()
	transaction.CloseSellOrder()

	//adiciona a transação na lista de transações
	b.Transactions = append(b.Transactions, transaction)
}
