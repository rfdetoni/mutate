package main

import "testing"

func TestValidateLocalURL(t *testing.T) {
 good:="http://127.0.0.1:39241/3ce729a6d10b4483bdf0d0a6/"
 if err:=validateLocalURL(good);err!=nil{t.Fatal(err)}
 for _,input:=range []string{
  "https://127.0.0.1:39241/3ce729a6d10b4483bdf0d0a6/",
  "http://127.0.0.1:39241/",
  "http://localhost:39241/3ce729a6d10b4483bdf0d0a6/",
  "http://192.168.1.20:39241/3ce729a6d10b4483bdf0d0a6/",
  "http://evil.example/3ce729a6d10b4483bdf0d0a6/",
  "http://127.0.0.1:39241/3ce729a6d10b4483bdf0d0a6/?x=1",
 }{
  if err:=validateLocalURL(input);err==nil{t.Errorf("accepted unsafe URL %s",input)}
 }
}
