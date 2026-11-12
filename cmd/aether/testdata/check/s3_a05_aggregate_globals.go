// aether:expect run

package main

// Package-level arrays and structs: zero at startup, initialized in
// dependency order, by literals and by calls, and changed by functions.

type Account struct {
	ID      int
	Balance int64
	Tags    [2]string
}

var accounts [4]Account
var total = sumAll()
var first = Account{ID: 1, Balance: 100, Tags: [2]string{"a", "b"}}
var table = squares()
var origin struct{ x, y int }

func squares() [6]int {
	var t [6]int
	for i := 0; i < 6; i++ {
		t[i] = i * i
	}
	return t
}

func sumAll() int64 {
	var s int64
	for i := 0; i < len(accounts); i++ {
		s += accounts[i].Balance
	}
	return s
}

func deposit(i int, amount int64) {
	accounts[i].Balance += amount
	accounts[i].ID = i + 1
}

func main() {
	println(total, first.ID, first.Balance, first.Tags[1], table[5], origin.x, origin.y)
	deposit(2, 50)
	deposit(3, -5)
	deposit(2, 1)
	println(sumAll(), accounts[2].ID, accounts[2].Balance, accounts[3].Balance, accounts[0].ID)
	accounts[0] = first
	first.Balance = 0
	println(accounts[0].Balance, first.Balance, accounts[0].Tags[0])
	origin.y = -3
	table[0] = table[5] + origin.y
	println(table[0], origin.y)
}
