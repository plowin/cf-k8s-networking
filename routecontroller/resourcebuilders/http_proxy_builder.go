package resourcebuilders

import (
	"errors"
	"fmt"

	istionetworkingv1alpha3 "code.cloudfoundry.org/cf-k8s-networking/routecontroller/apis/istio/networking/v1alpha3"
	hpv1 "github.com/projectcontour/contour/apis/projectcontour/v1"

	networkingv1alpha1 "code.cloudfoundry.org/cf-k8s-networking/routecontroller/apis/networking/v1alpha1"
	istiov1alpha3 "istio.io/api/networking/v1alpha3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// type K8sResource interface{}

// // "mesh" is a special reserved word on Istio VirtualServices
// // https://istio.io/docs/reference/config/networking/v1alpha3/virtual-service/#VirtualService
// const MeshInternalGateway = "mesh"

// // Istio destination weights are percentage based and must sum to 100%
// // https://istio.io/docs/concepts/traffic-management/
// const IstioExpectedWeight = int(100)

type HttpProxyBuilder struct {
	// IstioGateways []string
}

// // virtual service names cannot contain special characters
// func VirtualServiceName(fqdn string) string {
// 	sum := sha256.Sum256([]byte(fqdn))
// 	return fmt.Sprintf("vs-%x", sum)
// }

func (b *HttpProxyBuilder) BuildMutateFunction(actualVirtualService, desiredVirtualService *istionetworkingv1alpha3.VirtualService) controllerutil.MutateFn {
	return func() error {
		actualVirtualService.ObjectMeta.Labels = desiredVirtualService.ObjectMeta.Labels
		actualVirtualService.ObjectMeta.Annotations = desiredVirtualService.ObjectMeta.Annotations
		actualVirtualService.ObjectMeta.OwnerReferences = desiredVirtualService.ObjectMeta.OwnerReferences
		actualVirtualService.Spec = desiredVirtualService.Spec
		return nil
	}
}

func (b *HttpProxyBuilder) Build(routes *networkingv1alpha1.RouteList) ([]hpv1.HTTPProxy, error) {
	resources := []hpv1.HTTPProxy{}
	// resources := []istionetworkingv1alpha3.VirtualService{}

	routesForFQDN := groupByFQDN(routes)
	sortedFQDNs := sortFQDNs(routesForFQDN)

	for _, fqdn := range sortedFQDNs {
		virtualService, err := b.fqdnToVirtualService(fqdn, routesForFQDN[fqdn])
		if err != nil {
			return []hpv1.HTTPProxy{}, err
		}

		resources = append(resources, virtualService)
	}

	return resources, nil
}

func (b *HttpProxyBuilder) fqdnToVirtualService(fqdn string, routes []networkingv1alpha1.Route) (hpv1.HTTPProxy, error) {
	name := VirtualServiceName(fqdn)
	hp := hpv1.HTTPProxy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: routes[0].ObjectMeta.Namespace,
			Labels:    map[string]string{},
			Annotations: map[string]string{
				"cloudfoundry.org/fqdn": fqdn,
			},
			OwnerReferences: []metav1.OwnerReference{},
		},
		Spec: hpv1.HTTPProxySpec{
			VirtualHost: &hpv1.VirtualHost{Fqdn: fqdn},
		},
	}

	err := validateRoutesForFQDN(routes)
	if err != nil {
		return hpv1.HTTPProxy{}, err
	}

	// if routes[0].Spec.Domain.Internal {
	// 	hp.Spec.
	// 	vs.Spec.Gateways = []string{MeshInternalGateway}
	// } else {
	// 	vs.Spec.Gateways = b.IstioGateways
	// }

	sortRoutes(routes)

	for _, route := range routes {
		hp.ObjectMeta.OwnerReferences = append(hp.ObjectMeta.OwnerReferences, routeToOwnerRef(&route))
		hpRoute = hpv1.Route{}

		if len(route.Spec.Destinations) != 0 {
			//TODO point to the new function we wrote
			istioDestinations, err := destinationsToHttpRouteDestinations(route, route.Spec.Destinations)
			if err != nil {
				return hpv1.HTTPProxy{}, err
			}

			istioRoute.Route = istioDestinations
		} else if len(routes) > 1 {
			continue
		} else {
			istioRoute.Route = httpRouteDestinationPlaceholder()
		}

		if route.Spec.Path != "" {
			istioRoute.Match = []*istiov1alpha3.HTTPMatchRequest{
				{
					Uri: &istiov1alpha3.StringMatch{
						MatchType: &istiov1alpha3.StringMatch_Prefix{
							Prefix: route.Spec.Path,
						},
					},
				},
			}
		}

		vs.Spec.Http = append(vs.Spec.Http, &istioRoute)
	}

	return hp, nil
}

func validateRoutesForFQDN(routes []networkingv1alpha1.Route) error {
	for _, route := range routes {
		// We are assuming that internal and external routes cannot share an fqdn
		// Cloud Controller should validate and prevent this scenario
		if routes[0].Spec.Domain.Internal != route.Spec.Domain.Internal {
			msg := fmt.Sprintf(
				"route guid %s and route guid %s disagree on whether or not the domain is internal",
				routes[0].ObjectMeta.Name,
				route.ObjectMeta.Name)
			return errors.New(msg)
		}

		// Guard against two Routes for the same fqdn belonging to different namespaces
		if routes[0].ObjectMeta.Namespace != route.ObjectMeta.Namespace {
			msg := fmt.Sprintf(
				"route guid %s and route guid %s share the same FQDN but have different namespaces",
				routes[0].ObjectMeta.Name,
				route.ObjectMeta.Name)
			return errors.New(msg)
		}
	}

	return nil
}

func destinationsForFQDN(fqdn string, routesByFQDN map[string][]networkingv1alpha1.Route) []networkingv1alpha1.RouteDestination {
	destinations := make([]networkingv1alpha1.RouteDestination, 0)
	routes := routesByFQDN[fqdn]
	for _, route := range routes {
		destinations = append(destinations, route.Spec.Destinations...)
	}
	return destinations
}

func httpRouteDestinationPlaceholder() []*istiov1alpha3.HTTPRouteDestination {
	const PLACEHOLDER_NON_EXISTING_DESTINATION = "no-destinations"

	return []*istiov1alpha3.HTTPRouteDestination{
		&istiov1alpha3.HTTPRouteDestination{
			Destination: &istiov1alpha3.Destination{
				Host: PLACEHOLDER_NON_EXISTING_DESTINATION,
			},
		},
	}
}

func destinationsToHTTPProxyServices(route networkingv1alpha1.Route, destinations []networkingv1alpha1.RouteDestination) ([]hpv1.Service, error) {
	err := validateWeights(route, destinations)
	if err != nil {
		return nil, err
	}

	httpDestinations := make([]hpv1.Service, 0)
	for _, destination := range destinations {
		httpDestination := hpv1.Service{
			Name: serviceName(destination),
			Port: 8080, // services default to 8080
			RequestHeadersPolicy: &hpv1.HeadersPolicy{
				Set: []hpv1.HeaderValue{{
					Name:  "CF-App-Id",
					Value: destination.App.Guid,
				}, {
					Name:  "CF-Space-Id",
					Value: route.ObjectMeta.Labels["cloudfoundry.org/space_guid"],
				}, {
					Name:  "CF-Organization-Id",
					Value: route.ObjectMeta.Labels["cloudfoundry.org/org_guid"],
				}},
			},
		}

		if destination.Weight != nil {
			httpDestination.Weight = int64(*destination.Weight)
		}
		httpDestinations = append(httpDestinations, httpDestination)
	}
	if destinations[0].Weight == nil {
		n := len(destinations)
		for i, _ := range httpDestinations {
			weight := int(IstioExpectedWeight / n)
			if i == 0 {
				// pad the first destination's weight to ensure all weights sum to 100
				remainder := IstioExpectedWeight - n*weight
				weight += remainder
			}
			httpDestinations[i].Weight = int64(weight)
		}
	}
	return httpDestinations, nil
}
