package loader

import (
	"encoding/xml"
	"os"
	"path"

	"github.com/pkg/errors"
)

const containerFilePath = "META-INF/container.xml"

type Container struct {
	Rootfile struct {
		FullPath string `xml:"full-path,attr"`
	} `xml:"rootfiles>rootfile"`
}

type Package struct {
	XMLName  xml.Name `xml:"package"`
	Metadata Metadata `xml:"metadata"`
	Manifest Manifest `xml:"manifest"`
	Spine    Spine    `xml:"spine"`
}

type Metadata struct {
	Title       string `xml:"http://purl.org/dc/elements/1.1/ title" json:"title"`
	Identifier  string `xml:"http://purl.org/dc/elements/1.1/ identifier" json:"identifier"`
	Language    string `xml:"http://purl.org/dc/elements/1.1/ language" json:"language"`
	Creator     string `xml:"http://purl.org/dc/elements/1.1/ creator" json:"creator"`
	Publisher   string `xml:"http://purl.org/dc/elements/1.1/ publisher" json:"publisher"`
	Description string `xml:"http://purl.org/dc/elements/1.1/ description" json:"description"`
	Metas       []Meta `xml:"meta" json:"metas"`
}

type Meta struct {
	Property string `xml:"property,attr" json:"property"`
	Refines  string `xml:"refines,attr,omitempty" json:"refines"`
	Scheme   string `xml:"scheme,attr,omitempty" json:"scheme"`
	Content  string `xml:",chardata" json:"content"`
}

type Manifest struct {
	Items []Item `xml:"item" json:"items"`
}

// GetItemByID returns the item with the given ID.
// Returns nil if no item is found or if id is empty.
func (m Manifest) GetItemByID(id string) *Item {
    if id == "" {
        return nil
    }
    for i := range m.Items {
        if m.Items[i].ID == id {
            return &m.Items[i]  // Return reference to slice element
        }
    }
    return nil
}

type Item struct {
	Href       string `xml:"href,attr" json:"href"`
	ID         string `xml:"id,attr" json:"id"`
	MediaType  string `xml:"media-type,attr" json:"mediaType"`
	Properties string `xml:"properties,attr,omitempty" json:"properties"`
}

type Spine struct {
	Toc      string    `xml:"toc,attr" json:"toc"`
	ItemRefs []ItemRef `xml:"itemref" json:"itemRefs"`
}

type ItemRef struct {
	IDRef string `xml:"idref,attr" json:"IDRef"`
}

func ParseContainer(filePath string) (*Container, error) {
	if filePath == "" {
        return nil, errors.New("filePath cannot be empty")
    }

	file, err := os.Open(path.Join(filePath, containerFilePath))
	if err != nil {
		return nil, errors.WithMessage(err, "failed to open container file")
	}
	defer file.Close()

	var container Container
	if err := xml.NewDecoder(file).Decode(&container); err != nil {
		return nil, errors.WithMessage(err, "failed to decode container")
	}

	return &container, nil
}

func ParsePackage(filePath string) (*Package, error) {
	if filePath == "" {
        return nil, errors.New("filePath cannot be empty")
    }
	
	file, err := os.Open(filePath)
	if err != nil {
		return nil, errors.WithMessage(err, "failed to open package file")
	}
	defer file.Close()

	var pkg Package
	if err := xml.NewDecoder(file).Decode(&pkg); err != nil {
		return nil, errors.WithMessage(err, "failed to decode package")
	}

	return &pkg, nil
}
