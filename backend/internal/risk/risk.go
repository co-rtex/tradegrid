// Package risk provides pure, checked reservation and settlement arithmetic.
package risk

import (
 "errors"
 "fmt"
 "math"

 "github.com/co-rtex/tradegrid/backend/internal/orders"
)

const MaxAggregate int64 = 9007199254740991

var (
 ErrInvalid = errors.New("risk: invalid amount or balance")
 ErrInsufficient = errors.New("risk: insufficient available assets")
 ErrLimit = errors.New("risk: arithmetic or aggregate limit exceeded")
 ErrUnknownAccount = errors.New("risk: unknown account")
)

type Balance struct {
 Total int64
 Reserved int64
}
func (b Balance) Available() int64 { return b.Total - b.Reserved }
func (b Balance) valid() bool { return b.Total >= 0 && b.Total <= MaxAggregate && b.Reserved >= 0 && b.Reserved <= b.Total }
type Account struct {
 Cash Balance
 Positions map[string]Balance
}

func clone(a Account) Account {
 out:=Account{Cash:a.Cash,Positions:make(map[string]Balance,len(a.Positions))}
 for k,v:=range a.Positions { out.Positions[k]=v }
 return out
}
func validAccount(a Account) bool {
 if !a.Cash.valid() {return false}
 for _,v:=range a.Positions {if !v.valid(){return false}}
 return true
}
func checkedAdd(a,b int64)(int64,error){
 if b>0 && a>math.MaxInt64-b || b<0 && a<math.MinInt64-b {return 0,ErrLimit}
 v:=a+b
 if v<0 || v>MaxAggregate {return 0,ErrLimit}
 return v,nil
}
func checkedProduct(a,b int64)(int64,error) {
 if a<=0 || b<=0 {return 0,ErrInvalid}
 if a>MaxAggregate/b {return 0,ErrLimit}
 return a*b,nil
}

// Reserve returns a new account snapshot; the supplied account is unchanged.
func Reserve(a Account,side orders.Side,symbol string,limitPrice,quantity int64)(Account,error){
 if !validAccount(a)||symbol=="" {return Account{},ErrInvalid}
 if !side.Valid(){return Account{},ErrInvalid}
 notional,err:=checkedProduct(limitPrice,quantity)
 if err!=nil{return Account{},err}
 out:=clone(a)
 if side==orders.SideBuy {
  if out.Cash.Available()<notional{return Account{},ErrInsufficient}
  out.Cash.Reserved,err=checkedAdd(out.Cash.Reserved,notional)
 } else {
  holding,ok:=out.Positions[symbol]
  if !ok || holding.Available()<quantity {return Account{},ErrInsufficient}
  holding.Reserved,err=checkedAdd(holding.Reserved,quantity)
  out.Positions[symbol]=holding
 }
 if err!=nil{return Account{},err}
 return out,nil
}

// Settle returns a fresh account map after one execution, or an error with no published changes.
// The buy order's limit determines the released reservation; the execution price determines payment.
// For self-trades, cash and shares net to zero, while both order reservations are released.
func Settle(accounts map[string]Account,buyerID,sellerID,symbol string,buyLimit,executionPrice,quantity int64)(map[string]Account,error){
 if buyerID==""||sellerID==""||symbol==""||executionPrice>buyLimit {return nil,ErrInvalid}
 reserved,err:=checkedProduct(buyLimit,quantity)
 if err!=nil{return nil,err}
 payment,err:=checkedProduct(executionPrice,quantity)
 if err!=nil{return nil,err}
 buyer,ok:=accounts[buyerID];if !ok{return nil,fmt.Errorf("%w: buyer",ErrUnknownAccount)}
 seller,ok:=accounts[sellerID];if !ok{return nil,fmt.Errorf("%w: seller",ErrUnknownAccount)}
 if !validAccount(buyer)||!validAccount(seller){return nil,ErrInvalid}
 out:=make(map[string]Account,len(accounts))
 for id,a:=range accounts {out[id]=clone(a)}
 b:=out[buyerID];s:=out[sellerID]
 if b.Cash.Reserved<reserved || s.Positions[symbol].Reserved<quantity {return nil,ErrInsufficient}
 if buyerID==sellerID {
  holding:=b.Positions[symbol]
  b.Cash.Reserved-=reserved
  holding.Reserved-=quantity
  b.Positions[symbol]=holding
  out[buyerID]=b
  return out,nil
 }
 b.Cash.Total-=payment
 b.Cash.Reserved-=reserved
 bh:=b.Positions[symbol]
 bh.Total,err=checkedAdd(bh.Total,quantity);if err!=nil{return nil,err}
 b.Positions[symbol]=bh
 s.Cash.Total,err=checkedAdd(s.Cash.Total,payment);if err!=nil{return nil,err}
 sh:=s.Positions[symbol]
 sh.Total-=quantity
 sh.Reserved-=quantity
 s.Positions[symbol]=sh
 if !validAccount(b)||!validAccount(s) {return nil,ErrInsufficient}
 out[buyerID]=b
 out[sellerID]=s
 return out,nil
}
