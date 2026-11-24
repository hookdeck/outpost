package destregistry

// CloseForTest stops the publisher cache's cleanup goroutine.
func CloseForTest(r Registry) {
	r.(*registry).publishers.Close()
}
