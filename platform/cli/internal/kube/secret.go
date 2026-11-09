package kube

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// UpsertSecret creates the Secret, or replaces the existing one's type, labels and data wholesale
// (keys not in s are dropped).
func (c *Clients) UpsertSecret(ctx context.Context, s *corev1.Secret) error {
	api := c.Core.CoreV1().Secrets(s.Namespace)
	cur, err := api.Get(ctx, s.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = api.Create(ctx, s, metav1.CreateOptions{FieldManager: FieldManager})
		if err != nil {
			return fmt.Errorf("create secret %s/%s: %w", s.Namespace, s.Name, err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("get secret %s/%s: %w", s.Namespace, s.Name, err)
	}
	if cur.Type != s.Type { // type is immutable
		if err := api.Delete(ctx, s.Name, metav1.DeleteOptions{}); err != nil {
			return fmt.Errorf("replace secret %s/%s: %w", s.Namespace, s.Name, err)
		}
		_, err = api.Create(ctx, s, metav1.CreateOptions{FieldManager: FieldManager})
		return err
	}
	cur.Labels, cur.Data, cur.StringData = s.Labels, s.Data, nil
	if _, err := api.Update(ctx, cur, metav1.UpdateOptions{FieldManager: FieldManager}); err != nil {
		return fmt.Errorf("update secret %s/%s: %w", s.Namespace, s.Name, err)
	}
	return nil
}
