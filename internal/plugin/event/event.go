package event

var publisher func(string, any) bool

func SetPublisher(fn func(string, any) bool) { publisher = fn }

func Publish(event string, payload any) bool {
	if publisher == nil {
		return false
	}
	return publisher(event, payload)
}
