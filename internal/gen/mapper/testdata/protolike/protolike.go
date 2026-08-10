// Package protolike mimics protoc-generated open API structs: exported
// fields, nil-safe getters, and unexported bookkeeping fields.
package protolike

// Employee mirrors model.Employee on the wire side.
type Employee struct {
	state         int
	Id            string
	Name          string
	Age           int32
	HiredAt       *Date
	Address       *Address
	Tags          []string
	Subordinates  []*Employee
	Note          *string
	unknownFields []byte
}

func (x *Employee) GetId() string {
	if x != nil {
		return x.Id
	}
	return ""
}

func (x *Employee) GetName() string {
	if x != nil {
		return x.Name
	}
	return ""
}

func (x *Employee) GetAge() int32 {
	if x != nil {
		return x.Age
	}
	return 0
}

func (x *Employee) GetHiredAt() *Date {
	if x != nil {
		return x.HiredAt
	}
	return nil
}

func (x *Employee) GetAddress() *Address {
	if x != nil {
		return x.Address
	}
	return nil
}

func (x *Employee) GetTags() []string {
	if x != nil {
		return x.Tags
	}
	return nil
}

func (x *Employee) GetSubordinates() []*Employee {
	if x != nil {
		return x.Subordinates
	}
	return nil
}

// GetNote dereferences the proto3 optional field, like protoc does.
func (x *Employee) GetNote() string {
	if x != nil && x.Note != nil {
		return *x.Note
	}
	return ""
}

// Date mirrors google.type.Date.
type Date struct {
	state int
	Year  int32
	Month int32
	Day   int32
}

func (x *Date) GetYear() int32 {
	if x != nil {
		return x.Year
	}
	return 0
}

func (x *Date) GetMonth() int32 {
	if x != nil {
		return x.Month
	}
	return 0
}

func (x *Date) GetDay() int32 {
	if x != nil {
		return x.Day
	}
	return 0
}

// Address mirrors model.Address.
type Address struct {
	state  int
	City   string
	Street string
}

func (x *Address) GetCity() string {
	if x != nil {
		return x.City
	}
	return ""
}

func (x *Address) GetStreet() string {
	if x != nil {
		return x.Street
	}
	return ""
}

// Flat is a plain counterpart of model.WithBase without embedding.
type Flat struct {
	Code string
	Name string
}
