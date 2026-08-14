package lib

type Greeter struct {
	Name string
}

func NewGreeter(name string) Greeter {
	return Greeter{Name: name}
}

func (g Greeter) Message() string {
	return "Hello, " + g.Name
}
