package packs

func PortsPriority() []int {
	return []int{80, 8080}
}

func NamePriority() []string {
	return []string{"www", "web", "gateway"}
}
