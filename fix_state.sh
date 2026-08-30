cat state/state_test.go | grep -v "<<<<<<<" | grep -v "=======" | grep -v ">>>>>>>" > state/state_test_fixed.go
mv state/state_test_fixed.go state/state_test.go
