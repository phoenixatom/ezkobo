package book

import (
	"archive/zip"
	"context"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/beevik/etree"
	"github.com/pgaskin/kepubify/v4/kepub"
)

type epubMeta struct {
	Title    string
	Author   string
	HasCover bool
	Cover    []byte // a new cover to add, if any
}

// readEPUBMeta reads title, author and whether a cover exists from the OPF.
func readEPUBMeta(file string) (*epubMeta, error) {
	zr, err := zip.OpenReader(file)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	opfPath, doc, err := readOPF(&zr.Reader)
	if err != nil {
		return nil, err
	}
	_ = opfPath
	m := &epubMeta{}
	if md := doc.FindElement("//metadata"); md != nil {
		if e := md.FindElement("title"); e != nil {
			m.Title = strings.TrimSpace(e.Text())
		}
		if e := md.FindElement("creator"); e != nil {
			m.Author = strings.TrimSpace(e.Text())
		}
		for _, e := range md.SelectElements("meta") {
			if e.SelectAttrValue("name", "") == "cover" {
				m.HasCover = true
			}
		}
	}
	for _, e := range doc.FindElements("//manifest/item") {
		if strings.Contains(e.SelectAttrValue("properties", ""), "cover-image") {
			m.HasCover = true
		}
	}
	return m, nil
}

func readOPF(zr *zip.Reader) (string, *etree.Document, error) {
	container, err := readZipFile(zr, "META-INF/container.xml")
	if err != nil {
		return "", nil, err
	}
	cdoc := etree.NewDocument()
	if err := cdoc.ReadFromBytes(container); err != nil {
		return "", nil, err
	}
	rf := cdoc.FindElement("//rootfile")
	if rf == nil {
		return "", nil, errors.New("no rootfile in container.xml")
	}
	opfPath := rf.SelectAttrValue("full-path", "")
	opf, err := readZipFile(zr, opfPath)
	if err != nil {
		return "", nil, err
	}
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(opf); err != nil {
		return "", nil, err
	}
	return opfPath, doc, nil
}

func readZipFile(zr *zip.Reader, name string) ([]byte, error) {
	f, err := zr.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, 8<<20))
}

// writeEPUBMeta rewrites the EPUB at file with m's title, author and cover.
func writeEPUBMeta(file string, m *epubMeta) error {
	zr, err := zip.OpenReader(file)
	if err != nil {
		return err
	}
	defer zr.Close()
	opfPath, doc, err := readOPF(&zr.Reader)
	if err != nil {
		return err
	}
	md := doc.FindElement("//metadata")
	if md == nil {
		return errors.New("no metadata in OPF")
	}
	setDC(md, "title", m.Title)
	setDC(md, "creator", m.Author)

	coverPath := ""
	if m.Cover != nil && !m.HasCover {
		if manifest := doc.FindElement("//manifest"); manifest != nil {
			item := manifest.CreateElement("item")
			item.CreateAttr("id", "ezkobo-cover")
			item.CreateAttr("href", "ezkobo-cover.jpg")
			item.CreateAttr("media-type", "image/jpeg")
			item.CreateAttr("properties", "cover-image") // EPUB 3
			meta := md.CreateElement("meta")             // EPUB 2
			meta.CreateAttr("name", "cover")
			meta.CreateAttr("content", "ezkobo-cover")
			coverPath = path.Join(path.Dir(opfPath), "ezkobo-cover.jpg")
		}
	}
	doc.Indent(2)
	opf, err := doc.WriteToBytes()
	if err != nil {
		return err
	}

	out, err := os.CreateTemp(filepath.Dir(file), ".ezkobo-*.part")
	if err != nil {
		return err
	}
	defer os.Remove(out.Name())
	zw := zip.NewWriter(out)
	for _, f := range zr.File {
		if f.Name == opfPath {
			w, err := zw.CreateHeader(&zip.FileHeader{Name: f.Name, Method: zip.Deflate, Modified: f.Modified})
			if err != nil {
				return err
			}
			w.Write(opf)
			continue
		}
		if err := zw.Copy(f); err != nil { // keeps "mimetype" first and stored
			return err
		}
	}
	if coverPath != "" {
		w, err := zw.Create(coverPath)
		if err != nil {
			return err
		}
		w.Write(m.Cover)
	}
	if err := zw.Close(); err != nil {
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	zr.Close()
	return os.Rename(out.Name(), file)
}

// setDC sets the text of the first dc:<name> element, creating it if needed.
func setDC(md *etree.Element, name, value string) {
	if value == "" {
		return
	}
	e := md.FindElement(name)
	if e == nil {
		e = md.CreateElement("dc:" + name)
	}
	e.SetText(value)
}

func convertKepub(ctx context.Context, file string) error {
	zr, err := zip.OpenReader(file)
	if err != nil {
		return err
	}
	defer zr.Close()
	out, err := os.CreateTemp(filepath.Dir(file), ".ezkobo-*.part")
	if err != nil {
		return err
	}
	defer os.Remove(out.Name())
	if err := kepub.NewConverter().Convert(ctx, out, &zr.Reader); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	zr.Close()
	return os.Rename(out.Name(), file)
}
