package jsapi

/*
#cgo LDFLAGS: -L./lib -ljsapi -l:libjs.a -lpthread -lstdc++ -ldl
#include <stdlib.h>
#include "lib/js.hpp"
void Init();
*/
import "C"
import (
	"fmt"
	"unsafe"
	"reflect"
	"runtime"
	"encoding/json"
	"sync"
	"io"
	"io/ioutil"
	"os"
)

var jsapi *api

type fn struct {
	call func()
	done chan bool
}

type api struct {
	in chan *fn
}

func (jsapi *api) do(callback func()) {
	if C.JSAPI_ThreadCanAccessRuntime() == C.JSAPI_OK {
		callback()
		return
	}
	fn := &fn{
		call: callback,
		done: make(chan bool, 1),
	}
	jsapi.in <- fn
	<-fn.done
}

func start() *api {
	jsapi := &api{
		in: make(chan *fn),
	}
	ready := make(chan bool)
	go func(){
		runtime.LockOSThread()
		C.Init()
		if C.JSAPI_Init() != C.JSAPI_OK {
			panic("could not init JSAPI runtime")
		}
		ready <- true
		for {
			select {
			case fn := <-jsapi.in:
				fn.call()
				fn.done <- true
			}
		}

	}()
	<-ready
	return jsapi
}

func init() {
	jsapi = start()
}


var contexts = make(map[*C.JSAPIContext]*Context)



type destroyer interface {
	Destroy()
}

func finalizer(x destroyer){
	x.Destroy()
}

//export callback
func callback(c *C.JSAPIContext, id *C.JSObject, cname *C.char, args *C.char, argn C.int, out **C.char) C.int {
	cx, ok := contexts[c]
	if !ok {
		*out = C.CString("attempt to use context after destroyed")
		return 0
	}
	name := C.GoString(cname)
	var fn *Func
	if id == c.o {
		fn, ok = cx.funcs[name]
		if !ok {
			*out = C.CString("attempt to use global func that doesn't appear to exist")
			return 0
		}
	} else {
		o, ok := cx.objs[id]
		if !ok {
			fmt.Println("obj=", id)
			*out = C.CString("attempt to use global object that doesn't appear to exist")
			return 0
		}
		fn, ok = o.funcs[name]
		if !ok {
			*out = C.CString("attempt to use func that doesn't appear to exist")
			return 0
		}
	}
	json := C.GoStringN(args,argn)
	outjson,err := fn.Call(json)
	if err != nil {
		*out = C.CString(err.Error())
		return 0
	}
	*out = C.CString(outjson)
	return 1
}

//export reporter
func reporter(c *C.JSAPIContext, cfilename *C.char, lineno C.uint, cmsg *C.char) {
	cx, ok := contexts[c]
	if !ok {
		return
	}
	cx.setError(C.GoString(cfilename), uint(lineno), C.GoString(cmsg))
}

//export getprop
func getprop(c *C.JSAPIContext, id *C.JSObject, cname *C.char, out **C.char) C.int {
	cx, ok := contexts[c]
	if !ok {
		*out = C.CString("attempt to use context after destroyed")
		return 0
	}
	o, ok := cx.objs[id]
	if !ok {
		fmt.Println("bad object id", id)
		*out = C.CString("attempt to use object that doesn't appear to exist")
		return 0
	}
	p, ok := o.props[C.GoString(cname)]
	if !ok {
		*out = C.CString("attempt to get property that doesn't appear to exist")
		return 0
	}
	outjson,err := p.get()
	if err != nil {
		*out = C.CString(err.Error())
		return 0
	}
	*out = C.CString(outjson)
	return 1
}

//export setprop
func setprop(c *C.JSAPIContext, id *C.JSObject, cname *C.char, val *C.char, valn C.int, out **C.char) C.int {
	cx, ok := contexts[c]
	if !ok {
		*out = C.CString("attempt to use context after destroyed")
		return 0
	}
	o, ok := cx.objs[id]
	if !ok {
		*out = C.CString("attempt to use object that doesn't appear to exist")
		return 0
	}
	p, ok := o.props[C.GoString(cname)]
	if !ok {
		*out = C.CString("attempt to set property that doesn't appear to exist")
		return 0
	}
	json := C.GoStringN(val,valn)
	outjson,err := p.set(json)
	if err != nil {
		*out = C.CString(err.Error())
		return 0
	}
	*out = C.CString(outjson)
	return 1
}

type ErrorReport struct {
	Filename string
	Line uint
	Message string
}

func (err *ErrorReport) Error() string {
	return fmt.Sprintf("%s:%d %s", err.Filename, err.Line, err.Message)
}

func (err *ErrorReport) String() string {
	return err.Message
}

type Context struct {
	ptr *C.JSAPIContext
	objs map[*C.JSObject]*Object
	funcs map[string]*Func
	Valid bool
	errs map[string]*ErrorReport
	mu sync.Mutex
}

// The javascript side ends up calling this when an uncaught
// exception manages to bubble to the top.
func (cx *Context) setError(filename string, line uint, message string) {
	if cx.errs == nil {
		cx.errs = make(map[string]*ErrorReport)
	}
	cx.errs[filename] = &ErrorReport{
		Filename: filename,
		Line: line,
		Message: message,
	}
}

// fetch an error for an eval filename and remove it from the pile
func (cx *Context) getError(filename string) *ErrorReport {
	if err, ok := cx.errs[filename]; ok {
		delete(cx.errs, filename)
		return err
	}
	if err, ok := cx.errs["__fatal__"]; ok {
		delete(cx.errs, filename)
		return err
	}
	return nil
}

func (cx *Context) Destroy() {
	if cx.Valid {
		// do
		cx.do(func(){
			C.JSAPI_DestroyContext(cx.ptr)
			cx.Valid = false
			cx.ptr = nil
		})
	}
}

// Execute javascript source in Context and discard any response
func (cx *Context) Exec(source string) (err error) {
	cx.do(func(){
		csource := C.CString(source)
		defer C.free(unsafe.Pointer(csource))
		filename := "eval"
		cfilename := C.CString(filename)
		defer C.free(unsafe.Pointer(cfilename))
		// eval
		if C.JSAPI_Eval(cx.ptr, csource, cfilename) != C.JSAPI_OK {
			if err = cx.getError(filename); err != nil {
				return
			}
			err = fmt.Errorf("Failed to exec javascript and no error report found")
			return
		}
	})
	return err
}

// Execute javascript source in Context and scan the response into result.
// Scanning follows the rules of json.Unmarshal so most go native types are
// supported and complex javascript objects can be scanned by referancing structs.
func (cx *Context) Eval(source string, result interface{}) (err error) {
	cx.do(func(){
		// alloc C-string
		csource := C.CString(source)
		defer C.free(unsafe.Pointer(csource))
		var jsonData *C.char
		var jsonLen C.int
		filename := "eval"
		cfilename := C.CString(filename)
		defer C.free(unsafe.Pointer(cfilename))
		// eval
		if C.JSAPI_EvalJSON(cx.ptr, csource, cfilename, &jsonData, &jsonLen) != C.JSAPI_OK {
			if err = cx.getError(filename); err != nil {
				return
			}
			err = fmt.Errorf("Failed to eval javascript and no error report found")
			return
		}
		defer C.free(unsafe.Pointer(jsonData))
		// convert to go
		b := []byte(C.GoStringN(jsonData, jsonLen))
		err = json.Unmarshal(b, result)
	})
	return err
}

// Execute javascript in the context from an io.Reader.
func (cx *Context) ExecFrom(r io.Reader) (err error) {
	b, err := ioutil.ReadAll(r)
	if err != nil {
		return
	}
	return cx.Exec(string(b))
}

// Execute javascript in the context from a file
func (cx *Context) ExecFile(filename string) (err error) {
	f, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer f.Close()
	return cx.ExecFrom(f)
}

// Define a javascript object in the Context.
// If proxy is nil, then an empty js object is created.
//
// cx.DefineObject("x", nil) // equivilent to x = {};
//
// If proxy references a struct type, then a two-way binding of all public 
// fields within proxy the proxy object will be exposed to js via the 
// created object.
//
// typedef Person {
//     Name string
// }
//
// 
// 
// 
func (cx *Context) DefineObject(name string, proxy interface{}) *Object {
	return cx.defineObject(name, proxy, nil)
}

func (cx *Context) defineObject(name string, proxy interface{}, id *C.JSObject) *Object {
	o := &Object{}
	o.funcs = make(map[string]*Func)
	o.props = make(map[string]*prop)
	o.cx = cx
	cx.do(func(){
		cname := C.CString(name)
		defer C.free(unsafe.Pointer(cname))
		o.id = C.JSAPI_DefineObject(cx.ptr, id, cname)
		cx.objs[o.id] = o
		if proxy != nil {
			o.proxy = proxy
			ov := reflect.ValueOf(proxy)
			ot := ov.Type()
			if ot.Kind() == reflect.Ptr {
				ov = reflect.Indirect(ov)
				ot = ov.Type()
			}
			if ot.Kind() != reflect.Struct {
				panic("proxy object must be a kind of struct or pointer to a struct")
			}
			for i := 0; i<ot.NumField(); i++ {
				f := ot.Field(i)
				fv := ov.Field(i)
				o.props[f.Name] = &prop{f.Name, fv, f.Type}
				cpropname := C.CString(f.Name)
				defer C.free(unsafe.Pointer(cpropname))
				if C.JSAPI_DefineProperty(cx.ptr, o.id, cpropname) != C.JSAPI_OK {
					panic("failed to define property")
				}
			}
		}
	})
	return o
}

func (cx *Context) DefineFunction(name string, fun interface{}) *Func {
	f := cx.defineFunction(name, fun, nil)
	cx.funcs[f.Name] = f
	return f
}

func (cx *Context) defineFunction(name string, fun interface{}, id *C.JSObject) *Func {
	f := NewFunc(fun)
	cx.do(func(){
		cname := C.CString(name)
		defer C.free(unsafe.Pointer(cname))
		if C.JSAPI_DefineFunction(cx.ptr, id, cname) != C.JSAPI_OK {
			panic("failed to define function")
		}
		f.Name = name
	})
	return f
}

// Attempt to aquire mutex, then runs in primary thread.
// panics if Context is invalid
func (cx *Context) do(fn func()) {
	if !cx.Valid {
		panic("attempt to do a destroyed Context")
	}
	if !cx.Valid {
		panic("context destroyed while waiting for do")
	}
	jsapi.do(fn)
}


func NewContext() *Context {
	cx := &Context{}
	jsapi.do(func(){
		cx.ptr = C.JSAPI_NewContext()
		cx.Valid = true
		cx.objs = make(map[*C.JSObject]*Object)
		cx.funcs = make(map[string]*Func)
		contexts[cx.ptr] = cx
		runtime.SetFinalizer(cx, finalizer)
	})
	return cx
}

type Object struct {
	id *C.JSObject
	cx *Context
	funcs map[string]*Func
	props map[string]*prop
	proxy interface{}
}

func (o *Object) DefineFunction(name string, fun interface{}) *Func {
	f := o.cx.defineFunction(name, fun, o.id)
	o.funcs[f.Name] = f
	return f
}

func (o *Object) DefineObject(name string, proxy interface{}) *Object {
	return o.cx.defineObject(name, proxy, o.id)
}

type Func struct {
	Name string
	v reflect.Value
	t reflect.Type
}

func NewFunc(fun interface{}) *Func {
	f := &Func{}
	f.v = reflect.ValueOf(fun)
	if !f.v.IsValid() {
		panic("invalid function type")
	}
	f.t = f.v.Type()
	if f.t.Kind() != reflect.Func {
		panic("X is not a valid function type")
	}
	// check inarg types are acceptable
	for i := 0; i < f.t.NumIn(); i++ {
		switch f.t.In(i).Kind() {
		case reflect.Bool,reflect.Int,reflect.Int8,reflect.Int16,
			reflect.Int32,reflect.Int64,reflect.Uint,reflect.Uint8,
			reflect.Uint16,reflect.Uint32,reflect.Uint64,reflect.Float32,
			reflect.Float64,reflect.Interface,reflect.Map,reflect.Slice,
			reflect.String:
			// ok
		default:
			panic("X is not a valid argument type for javascript interop")
		}
	}
	f.Name = "[anon]"
	return f
}

func (f *Func) Call(in string) (out string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%s: %v", f.Name, r)
		}
	}()
	return f.call(in)
}

func (f *Func) call(in string) (out string, err error) {
	// decode args
	var inargs []interface{}
	err = json.Unmarshal([]byte(in), &inargs)
	if err != nil {
		return
	}
	// validate args
	if len(inargs) != f.t.NumIn() && !f.t.IsVariadic() {
		return "", fmt.Errorf("Invalid number of arguments: expected %d got %d", f.t.NumIn(), len(inargs))
	}
	invals := make([]reflect.Value, len(inargs))
	for i := 0; i < len(inargs); i++ {
		v := reflect.ValueOf(inargs[i])
		var t reflect.Type
		if f.t.IsVariadic() && i >= f.t.NumIn()-1 { // handle varargs
			t = f.t.In(f.t.NumIn()-1).Elem()
		} else {
			t = f.t.In(i)
		}
		v, err = cast(v, t)
		if err != nil {
			return
		}
		invals[i] = v
	}
	// call func
	outvals := f.v.Call(invals)
	switch len(outvals) {
	case 0:
		return "", nil
	case 1:
		b,err := json.Marshal(outvals[0].Interface())
		return string(b), err
	default:
		outargs := make([]interface{}, len(outvals))
		for i := 0; i < len(outvals); i++ {
			outargs[i] = outvals[i].Interface()
		}
		b,err := json.Marshal(outargs)
		return string(b), err
	}
}

// try to convert v to something that is assignable to type t
func cast(v reflect.Value, t reflect.Type) (reflect.Value, error) {
	if v.Type().Kind() == reflect.Ptr && t.Kind() != reflect.Ptr {
		v = reflect.Indirect(v)
	}
	if !v.Type().AssignableTo(t) {
		if !v.Type().ConvertibleTo(t) {
			return v, fmt.Errorf("cannot cast %s to %s", v.Type().Kind(), t.Kind())
		}
		v = v.Convert(t)
	}
	return v, nil
}

// prop is a wrapper around a struct's field's refelction
type prop struct {
	name string
	v reflect.Value
	t reflect.Type
}

// get json for property
func (p *prop) get() (string, error) {
	b,err := json.Marshal(p.v.Interface())
	return string(b), err
}

// set property via json
func (p *prop) set(injson string) (string, error) {
	var x interface{}
	err := json.Unmarshal([]byte(injson), &x)
	if err != nil {
		return "", err
	}
	xv := reflect.ValueOf(x)
	xv, err = cast(xv, p.t)
	if !p.v.CanSet() {
		return "", fmt.Errorf("property %s is not settable", p.name)
	}
	p.v.Set(xv)
	return p.get()
}

