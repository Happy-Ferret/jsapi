package jsapi

import (
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"
)

func BenchmarkEvalSngl(b *testing.B) {
	cx := NewContext()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			var result interface{}
			err := cx.Eval(script, &result)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

func TestInterfaces(t *testing.T) {
	var _ Evaluator = &Context{}
	var _ Definer = &Context{}
	var _ Definer = &Object{}
}

func TestEvalNumber(t *testing.T) {

	cx := NewContext()
	defer cx.Destroy()

	var i int
	err := cx.Eval(`1+1`, &i)
	if err != nil {
		t.Fatal(err)
	}

	if i != 2 {
		t.Fatalf("expected 1+1 to eval to 2 but got %d", i)
	}

}

func TestEvalString(t *testing.T) {

	cx := NewContext()
	defer cx.Destroy()

	var s string
	err := cx.Eval(`"h"+"ello"`, &s)
	if err != nil {
		t.Fatal(err)
	}

	if s != "hello" {
		t.Fatalf("expected to eval to the string \"hello\" got %s", s)
	}

}

func TestEvalDate(t *testing.T) {

	cx := NewContext()
	defer cx.Destroy()

	var v time.Time
	err := cx.Eval(`new Date('2012-01-01')`, &v)
	if err != nil {
		t.Fatal(err)
	}
	layout := "2006-01-02"
	if v.Format(layout) != "2012-01-01" {
		t.Fatalf("expected to eval to Date(2012-01-01) to time.Time got %s", v.Format(layout))
	}

}

func TestEvalErrors(t *testing.T) {

	cx := NewContext()
	defer cx.Destroy()

	err := cx.Exec(`throw new Error('ERROR1');`)
	if err == nil {
		t.Fatalf("expected an error to be returned")
	}
	r, ok := err.(*ErrorReport)
	if !ok {
		t.Fatalf("expected the error to be an ErrorReport but got: %T %v", err, err)
	}
	if r.Message != "Error: ERROR1" {
		t.Fatalf(`expected error message to be "Error: ERROR1" but got %q`, r.Message)
	}
}

func TestObjectWithIntFunction(t *testing.T) {

	cx := NewContext()
	defer cx.Destroy()

	math, _ := cx.DefineObject("math", nil)

	math.DefineFunction("add", func(a int, b int) int {
		return a + b
	})

	var i int
	err := cx.Eval(`math.add(1,2)`, &i)
	if err != nil {
		t.Fatal(err)
	}
	if i != 3 {
		t.Fatalf("expected math.add(1,2) to return 3 but got %d", i)
	}

}

func TestProxyObjectWithFunction(t *testing.T) {

	cx := NewContext()
	defer cx.Destroy()

	type person struct {
		Name string
	}
	p := &person{"bob"}
	math, _ := cx.DefineObject("math", p)

	math.DefineFunction("add", func(a int, b int) int {
		return a + b
	})

	var i int
	err := cx.Eval(`var m = math; (function(math){ return math.add(1,2) })(m)`, &i)
	if err != nil {
		t.Fatal(err)
	}
	if i != 3 {
		t.Fatalf("expected math.add(1,2) (on proxy onject) to return 3 but got %d", i)
	}

}

func TestObjectApplyFunction(t *testing.T) {

	cx := NewContext()
	defer cx.Destroy()

	math, _ := cx.DefineObject("math", nil)

	math.DefineFunction("add", func(a int, b int) int {
		return a + b
	})

	var i int
	err := cx.Eval(`math.add.apply(math,[1,2])`, &i)
	if err != nil {
		t.Fatal(err)
	}
	if i != 3 {
		t.Fatalf("expected math.add.apply(math, [1,2]) to return 3 but got %d", i)
	}

}

func TestNestedObjects(t *testing.T) {

	cx := NewContext()
	defer cx.Destroy()

	parent, _ := cx.DefineObject("parent", nil)
	child, _ := parent.DefineObject("child", nil)

	child.DefineFunction("greet", func() string {
		return "hello"
	})

	var s string
	err := cx.Eval(`parent.child.greet()`, &s)
	if err != nil {
		t.Fatal(err)
	}
	if s != "hello" {
		t.Fatalf(`expected parent.child.greet() to return "hello" but got %s`, s)
	}

}

func TestObjectWithVaridicFunction(t *testing.T) {

	cx := NewContext()
	defer cx.Destroy()

	obj, _ := cx.DefineObject("fmt", nil)

	obj.DefineFunction("sprintf", func(format string, args ...interface{}) string {
		return fmt.Sprintf(format, args...)
	})

	var s string
	err := cx.Eval(`fmt.sprintf('%.0f/%.0f/%s', 1, 2.0, "3")`, &s)
	if err != nil {
		t.Fatal(err)
	}
	if s != "1/2/3" {
		t.Fatalf(`expected to return "1/2/3" but got %q`, s)
	}

}

func TestGlobalVaridicFunction(t *testing.T) {

	cx := NewContext()
	defer cx.Destroy()

	cx.DefineFunction("sprintf", func(format string, args ...interface{}) string {
		return fmt.Sprintf(format, args...)
	})

	var s string
	err := cx.Eval(`sprintf('%.0f/%.0f/%s', 1, 2.0, "3")`, &s)
	if err != nil {
		t.Fatal(err)
	}
	if s != "1/2/3" {
		t.Fatalf(`expected to return "1/2/3" but got %q`, s)
	}

}

func TestSleepContext(t *testing.T) {

	cx := NewContext()
	defer cx.Destroy()

	cx.DefineFunction("sleep", func(ms int) {
		time.Sleep(time.Duration(ms) * time.Millisecond)
	})

	err := cx.Exec(`sleep(1)`)
	if err != nil {
		t.Fatal(err)
	}

}

func TestErrorsInFunction(t *testing.T) {

	cx := NewContext()
	defer cx.Destroy()

	obj, _ := cx.DefineObject("errs", nil)

	obj.DefineFunction("raise", func(msg string) {
		panic(msg)
	})

	err := cx.Exec(`errs.raise('BANG')`)
	if err == nil {
		t.Fatalf("expected an error to be returned")
	}
	r, ok := err.(*ErrorReport)
	if !ok {
		t.Fatalf("expected the error to be an ErrorReport but got: %T %v", err, err)
	}
	exp := fmt.Sprintf("Error: raise: BANG")
	if r.Message != exp {
		t.Fatalf(`expected error message to be %q but got %q`, exp, r.Message)
	}

}

func TestObjectProperties(t *testing.T) {

	type Person struct {
		Name string
		Age  int
	}

	cx := NewContext()
	defer cx.Destroy()

	person := &Person{"jeff", 22}

	cx.DefineObject("o", person)

	var s string
	err := cx.Eval(`o.name`, &s)
	if err != nil {
		t.Fatal(err)
	}
	if s != person.Name {
		t.Fatalf(`expected to get value of person.Name (%q) from js but got %q`, person.Name, s)
	}

	err = cx.Exec(`o.name = "geoff"`)
	if err != nil {
		t.Fatal(err)
	}
	if person.Name != "geoff" {
		t.Fatalf(`expected to set value of person.Name to "geoff" but got %q`, person.Name)
	}

	var i int
	err = cx.Eval(`o.age`, &i)
	if err != nil {
		t.Fatal(err)
	}
	if i != person.Age {
		t.Fatalf(`expected to get value of person.Age (%d) from js but got %v`, person.Age, i)
	}

	err = cx.Exec(`o.age = 25`)
	if err != nil {
		t.Fatal(err)
	}
	if person.Age != 25 {
		t.Fatalf(`expected to set value of person.Age to 25 but got %v`, person.Age)
	}

}

func TestProxyObjectWithUnexportedField(t *testing.T) {

	type Person struct {
		name string
		age  int
	}

	cx := NewContext()
	defer cx.Destroy()

	person := &Person{"jeff", 22}

	cx.DefineObject("o", person)

	var o interface{}
	err := cx.Eval(`o`, &o)
	if err != nil {
		t.Fatal(err)
	}

}

func TestOneContextManyGoroutines(t *testing.T) {

	if testing.Short() {
		t.Skip()
	}

	runtime.GOMAXPROCS(20)

	cx := NewContext()
	defer cx.Destroy()

	cx.DefineFunction("snooze", func(ms int) bool {
		time.Sleep(time.Duration(ms) * time.Millisecond)
		return true
	})

	wg := new(sync.WaitGroup)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				var ok bool
				err := cx.Eval(`snooze(0)`, &ok)
				if err != nil {
					t.Error(err)
					return
				}
				if !ok {
					t.Errorf("expected ok response")
					return
				}
			}
		}()
	}
	wg.Wait()

}

func TestManyContextManyGoroutines(t *testing.T) {

	if testing.Short() {
		t.Skip()
	}

	runtime.GOMAXPROCS(20)

	wg := new(sync.WaitGroup)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cx := NewContext()
			defer cx.Destroy()

			cx.DefineFunction("snooze", func(ms int) bool {
				time.Sleep(time.Duration(ms) * time.Millisecond)
				return true
			})
			for j := 0; j < 50; j++ {
				var ok bool
				err := cx.Eval(`snooze(0)`, &ok)
				if err != nil {
					t.Error(err)
					return
				}
				if !ok {
					t.Errorf("expected ok response")
					return
				}
			}
		}()
	}
	wg.Wait()

}

func TestExecFile(t *testing.T) {

	cx := NewContext()
	defer cx.Destroy()
	if err := cx.ExecFile("./jsapi_test1.js"); err != nil {
		t.Fatal(err)
	}

	var ok bool
	if err := cx.Eval(`test()`, &ok); err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatalf("expected test() function from jsapi_test1.js file to return true got false")
	}

}

func TestExecFileErrors(t *testing.T) {

	cx := NewContext()
	defer cx.Destroy()
	if err := cx.ExecFile("./jsapi_test2.js"); err == nil {
		t.Fatal("expected to throw error")
	}

}

func TestDeadlockCondition(t *testing.T) {

	cx := NewContext()
	defer cx.Destroy()
	cx.DefineFunction("mkfun", func() {
		cx.DefineFunction("dynamic", func() bool {
			return true
		})
	})
	if err := cx.Exec(`mkfun()`); err != nil {
		t.Fatal(err)
	}
	var ok bool
	if err := cx.Eval(`dynamic()`, &ok); err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal()
	}

}

func TestStructArgsPtr(t *testing.T) {

	cx := NewContext()
	defer cx.Destroy()

	type args struct {
		A  int
		B  int
		Ok bool
	}

	cx.DefineFunction("test", func(x *args) *args {
		x.Ok = (x.A + x.B) == 3
		return x
	})

	var y args
	err := cx.Eval(`test({A: 1, B: 2})`, &y)
	if err != nil {
		t.Fatal(err)
	}

	if !y.Ok {
		t.Fatalf("expected to be able to use a struct type in function args got response: %v", y)
	}

}

func TestStructArgsValue(t *testing.T) {

	cx := NewContext()
	defer cx.Destroy()

	type args struct {
		A  int
		B  int
		Ok bool
	}

	cx.DefineFunction("test", func(x args) args {
		x.Ok = (x.A + x.B) == 3
		return x
	})

	var y args
	err := cx.Eval(`test({A: 1, B: 2})`, &y)
	if err != nil {
		t.Fatal(err)
	}

	if !y.Ok {
		t.Fatalf("expected to be able to use a struct type in function args got response: %v", y)
	}

}

func TestRawReturnType(t *testing.T) {

	cx := NewContext()
	defer cx.Destroy()

	cx.DefineFunction("raw", func() Raw {
		return `{"ok":true}`
	})

	var res struct {
		Ok bool
	}
	err := cx.Eval(`raw()`, &res)
	if err != nil {
		t.Fatal(err)
	}

	if !res.Ok {
		t.Fatalf("expected to return raw json and have it converted")
	}

}

func TestRawEval(t *testing.T) {

	cx := NewContext()
	defer cx.Destroy()

	var res Raw
	err := cx.Eval(`(function(){ return {a:1, b:2} })()`, &res)
	if err != nil {
		t.Fatal(err)
	}

	if res != `{"a":1,"b":2}` {
		t.Fatalf("expected to return raw json from eval")
	}

}

