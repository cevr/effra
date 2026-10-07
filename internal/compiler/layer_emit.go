package compiler

import (
	"fmt"
	"strconv"
	"strings"
)

// The aggregate contains one typed field per checked selection. Constructors
// read only declared completed dependency fields; they never copy live state.
func (g *goEmitter) layer(plan LayerPlan) string {
	stateType := "efLayerState_" + plan.Name
	outputType := "efLayerOutputs_" + plan.Name
	fields := map[string]string{}
	services := map[string]string{}
	for i, node := range plan.Nodes {
		fields[node.ID] = fmt.Sprintf("n%d", i)
		services[node.Service] = node.ID
	}
	var out strings.Builder
	out.WriteString("type " + stateType + " struct { input efContext\n")
	for _, node := range plan.Nodes {
		out.WriteString(fields[node.ID] + " efService_" + node.Service + "\n")
	}
	out.WriteString("}\ntype " + outputType + " struct {\n")
	for _, service := range plan.Provides {
		out.WriteString("s_" + service + " efService_" + service + "\n")
	}
	out.WriteString("}\nfunc efLayer_" + plan.Name + "[A any](program efEffect[A]) efEffect[A] {\n")
	arguments := map[string][]string{}
	for i, node := range plan.Nodes {
		selection := plan.selected[node.ID]
		for j, argument := range selection.effective.Value.Args {
			value := g.expr(argument, false, voidTypeName, &out)
			name := fmt.Sprintf("efLayerArgument%d_%d", i, j)
			out.WriteString(name + " := " + value + "\n")
			arguments[node.ID] = append(arguments[node.ID], name)
		}
	}
	out.WriteString("plan := er.NewPlan(" + strconv.Quote(plan.ID) + ", []er.Node[" + stateType + "]{\n")
	for _, node := range plan.Nodes {
		dependencies := []string{}
		for _, dependency := range node.Dependencies {
			dependencies = append(dependencies, strconv.Quote(dependency))
		}
		out.WriteString("{Spec:er.NodeSpec{ID:" + strconv.Quote(node.ID) + ",Dependencies:[]er.NodeID{" + strings.Join(dependencies, ",") + "},Source:er.NodeSource{Offset:" + strconv.Itoa(node.SelectionSpan.Offset) + ",Length:" + strconv.Itoa(node.SelectionSpan.Length) + ",Line:" + strconv.Itoa(node.SelectionSpan.Line) + ",Column:" + strconv.Itoa(node.SelectionSpan.Column) + "}},Construct:func(fc *er.FiberContext,state *" + stateType + ") er.Exit[er.Unit]{\n")
		provider := plan.selected[node.ID].provider
		if providerConstructed(provider) {
			out.WriteString("ctx := state.input; ctx.Runtime=fc\n")
			for _, requirement := range normalized(provider.Services) {
				if dependency := services[requirement]; dependency != "" {
					out.WriteString("ctx.s_" + requirement + " = &state." + fields[dependency] + "\n")
				}
			}
			out.WriteString("value := er.Invoke(fc,efToRuntime(ctx,efProvider_" + provider.Name + "(" + strings.Join(arguments[node.ID], ",") + ")))\nif value.IsFailure(){return er.Propagate[er.Unit](value)}\nstate." + fields[node.ID] + " = value.Value\n")
		} else {
			arguments := ""
			if provider.Name == "TestClock" || provider.Name == "TestScheduler" {
				arguments = "er.CurrentTestScheduler(fc)"
			}
			out.WriteString("state." + fields[node.ID] + " = efProvider_" + provider.Name + "(" + arguments + ")\n")
		}
		out.WriteString("return er.Succeed(er.Unit{})\n}},\n")
	}
	out.WriteString("},func(input efContext) " + stateType + " {return " + stateType + "{input:input}},func(state *" + stateType + ") " + outputType + " {return " + outputType + "{\n")
	for _, service := range plan.Provides {
		out.WriteString("s_" + service + ":state." + fields[services[service]] + ",\n")
	}
	out.WriteString("}})\nreturn func(ctx efContext) er.Exit[A]{return er.Invoke(ctx.Runtime,er.Provide(plan,ctx,func(outputs " + outputType + ") er.Effect[A]{\n")
	for _, service := range plan.Provides {
		out.WriteString("ctx.s_" + service + " = &outputs.s_" + service + "\n")
	}
	out.WriteString("return efToRuntime(ctx,program)\n}))}\n}\n")
	return out.String()
}

func jsLayer(plan LayerPlan) string {
	fields := map[string]string{}
	services := map[string]string{}
	for i, node := range plan.Nodes {
		fields[node.ID] = fmt.Sprintf("n%d", i)
		services[node.Service] = node.ID
	}
	var out strings.Builder
	out.WriteString("const __ef_layer_" + plan.Name + " = program => {\n")
	arguments := map[string][]string{}
	for i, node := range plan.Nodes {
		for j, argument := range plan.selected[node.ID].effective.Value.Args {
			name := fmt.Sprintf("__ef_layerArgument%d_%d", i, j)
			out.WriteString("const " + name + " = " + jsExpr(argument, false) + ";\n")
			arguments[node.ID] = append(arguments[node.ID], name)
		}
	}
	out.WriteString("const plan={id:" + quoted(plan.ID) + ",init:()=>({}),nodes:[\n")
	for _, node := range plan.Nodes {
		provider := plan.selected[node.ID].provider
		construction := "Effect.sync(()=>({...__ef_provider_" + provider.Name + "}))"
		if providerConstructed(provider) {
			construction = "__ef_provider_" + provider.Name + "(" + strings.Join(arguments[node.ID], ",") + ")"
		}
		for _, requirement := range normalized(provider.Services) {
			if dependency := services[requirement]; dependency != "" {
				construction = "Effect.provideService(" + construction + ",__ef_service_" + requirement + ",state." + fields[dependency] + ")"
			}
		}
		construction = "Effect.map(" + construction + ",value=>{state." + fields[node.ID] + "=value;})"
		dependencies := []string{}
		for _, dependency := range node.Dependencies {
			dependencies = append(dependencies, quoted(dependency))
		}
		out.WriteString("{id:" + quoted(node.ID) + ",dependencies:[" + strings.Join(dependencies, ",") + "],construct:state=>" + construction + "},\n")
	}
	outputs := []string{}
	for _, service := range plan.Provides {
		outputs = append(outputs, "s_"+service+":state."+fields[services[service]])
	}
	out.WriteString("],expose:state=>({" + strings.Join(outputs, ",") + "})};\n")
	provided := "program"
	for _, service := range plan.Provides {
		provided = "Effect.provideService(" + provided + ",__ef_service_" + service + ",outputs.s_" + service + ")"
	}
	out.WriteString("return __ef_provideLayer(plan,outputs=>" + provided + ");\n};\n")
	return out.String()
}
