package risk

import (
 "errors"
 "reflect"
 "testing"

 "github.com/co-rtex/tradegrid/backend/internal/orders"
)

func seeded() map[string]Account {
 newAccount:=func()Account{return Account{Cash:Balance{Total:10000000},Positions:map[string]Balance{"AAPL":{Total:100},"MSFT":{Total:100}}}}
 return map[string]Account{"alice":newAccount(),"bob":newAccount()}
}
func TestReserveAndFullFill(t *testing.T){
 initial:=seeded()
 alice,err:=Reserve(initial["alice"],orders.SideSell,"AAPL",19000,10)
 if err!=nil {t.Fatal(err)}
 bob,err:=Reserve(initial["bob"],orders.SideBuy,"AAPL",19100,10)
 if err!=nil {t.Fatal(err)}
 if initial["alice"].Positions["AAPL"].Reserved!=0 {t.Fatal("mutated original")}
 initial["alice"]=alice;initial["bob"]=bob
 next,err:=Settle(initial,"bob","alice","AAPL",19100,19000,10)
 if err!=nil {t.Fatal(err)}
 if next["bob"].Cash.Total!=9810000||next["bob"].Cash.Reserved!=0||next["bob"].Positions["AAPL"].Total!=110 {t.Fatalf("buyer: %+v",next["bob"])}
 if next["alice"].Cash.Total!=10190000||next["alice"].Positions["AAPL"].Total!=90||next["alice"].Positions["AAPL"].Reserved!=0 {t.Fatalf("seller: %+v",next["alice"])}
 if initial["bob"].Cash.Total!=10000000 {t.Fatal("settlement mutated input")}
}
func TestPartialAndOutstandingReservations(t *testing.T){
 a:=seeded()
 b,e:=Reserve(a["bob"],orders.SideBuy,"AAPL",19050,25);if e!=nil{t.Fatal(e)}
 b,e=Reserve(b,orders.SideBuy,"MSFT",20000,5);if e!=nil{t.Fatal(e)}
 s,e:=Reserve(a["alice"],orders.SideSell,"AAPL",19000,10);if e!=nil{t.Fatal(e)}
 a["bob"]=b;a["alice"]=s
 next,e:=Settle(a,"bob","alice","AAPL",19050,19000,10);if e!=nil{t.Fatal(e)}
 if next["bob"].Cash.Total!=9810000 || next["bob"].Cash.Reserved!=385750 || next["bob"].Cash.Available()!=9424250 {t.Fatalf("partial buyer: %+v",next["bob"].Cash)}
}
func TestSameAccountNetting(t *testing.T){
 a:=seeded()
 x,e:=Reserve(a["alice"],orders.SideSell,"AAPL",19000,5);if e!=nil{t.Fatal(e)}
 x,e=Reserve(x,orders.SideBuy,"AAPL",19100,5);if e!=nil{t.Fatal(e)}
 a["alice"]=x
 next,e:=Settle(a,"alice","alice","AAPL",19100,19000,5);if e!=nil{t.Fatal(e)}
 if next["alice"].Cash.Total!=10000000 || next["alice"].Cash.Reserved!=0 || next["alice"].Positions["AAPL"].Total!=100 || next["alice"].Positions["AAPL"].Reserved!=0 {t.Fatalf("self fill: %+v",next["alice"])}
}
func TestInsufficientAndNoMutation(t *testing.T){
 a:=seeded()
 b:=a["bob"]
 b.Cash.Reserved=9900000
 if _,e:=Reserve(b,orders.SideBuy,"AAPL",20000,6);!errors.Is(e,ErrInsufficient){t.Fatalf("cash: %v",e)}
 b=a["bob"]
 pos:=b.Positions["AAPL"];pos.Reserved=99;b.Positions["AAPL"]=pos
 if _,e:=Reserve(b,orders.SideSell,"AAPL",10000,2);!errors.Is(e,ErrInsufficient){t.Fatalf("shares: %v",e)}
 original:=seeded()
 before:=seeded()
 if _,e:=Settle(original,"bob","alice","AAPL",19050,19000,10);!errors.Is(e,ErrInsufficient){t.Fatalf("not reserved: %v",e)}
 if !reflect.DeepEqual(original,before){t.Fatal("failed settlement mutated accounts")}
}
func TestBounds(t *testing.T){
 if _,e:=checkedProduct(MaxAggregate,2);!errors.Is(e,ErrLimit){t.Fatalf("overflow: %v",e)}
 a:=seeded()["alice"]
 if _,e:=Reserve(a,orders.SideBuy,"AAPL",0,1);!errors.Is(e,ErrInvalid){t.Fatal(e)}
 if _,e:=Reserve(a,orders.SideBuy,"AAPL",100,0);!errors.Is(e,ErrInvalid){t.Fatal(e)}
 if _,e:=Settle(seeded(),"bob","alice","AAPL",100,101,1);!errors.Is(e,ErrInvalid){t.Fatal(e)}
}
