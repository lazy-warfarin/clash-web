package app

import "testing"

func TestStoreProfileLifecycle(t *testing.T) {
	s, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p, err := s.CreateProfile(Profile{Name: "demo", Source: "local", Content: "proxies: []"})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ActivateProfile(p.ID); err != nil {
		t.Fatal(err)
	}
	got, err := s.Profile(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Active || got.Name != "demo" {
		t.Fatalf("unexpected profile: %+v", got)
	}
	if err = s.DeleteProfile(p.ID); err != nil {
		t.Fatal(err)
	}
}
